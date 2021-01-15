package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"kdwon/utils"
	"log"
	"math"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
)

var (
	DefaultUserAgent          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.12; rv:57.0) Gecko/20100101 Firefox/57.0"
	DefaultDownPath           = "download"
	DefaultGoroutineNum       = 10
	DefaultThunderPrefix      = "thunder://"
	DefaultHttpPrefix         = "http"
	DefaultDownLinkPrefixList = []string{DefaultHttpPrefix, DefaultThunderPrefix}
	DeadSignal                = []os.Signal{
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGKILL,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	}
)

type (
	KSDown struct {
		UrlList   []string `json:"url_list"`
		UserAgent string   `json:"user_agent"`
		ShowPrint bool     `json:"show_print"`
		Goroutine int      `json:"goroutine"`
		Download  string   `json:"download"`
		FilePart  chan *FilePart
		Done      chan *FilePart
		Stop      chan bool
	}

	FilePart struct {
		FileName  string `json:"file_name"`
		Url       string `json:"url"`
		FileSize  int    `json:"file_size"`
		PartNum   int    `json:"part_num"`
		Error     error  `json:"error"`
		DestPath  string `json:"dest_path"`
		Part      Part   `json:"part"`
		BlockNum  int    `json:"block_num"` // 分块数
		BlockId   string `json:"block_id"`  // 分块ID
		filePoint *os.File
	}
	Part struct {
		Start int64 `json:"start"`
		Stop  int   `json:"stop"`
	}
)

func NewKSDown(showPrint bool) (*KSDown, error) {

	if !Exists(DefaultDownPath) {
		if err := os.MkdirAll(DefaultDownPath, os.ModePerm); err != nil {
			return nil, err
		}
	}
	return &KSDown{
		ShowPrint: showPrint,
		UserAgent: DefaultUserAgent,
		Goroutine: DefaultGoroutineNum,
		Download:  DefaultDownPath,
		FilePart:  make(chan *FilePart, DefaultGoroutineNum),
		Stop:      make(chan bool),
		Done:      make(chan *FilePart),
	}, nil
}
func Exists(path string) bool {
	_, err := os.Stat(path) //os.Stat获取文件信息
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}
func HasPrefixInArray(str string, prefixList []string) string {
	for _, prefix := range prefixList {

		if strings.HasPrefix(strings.TrimSpace(str), prefix) {
			return prefix
		}
	}
	return ""
}
func (ks *KSDown) addTask(url []string) {

	for _, link := range url {

		if prefix := HasPrefixInArray(link, DefaultDownLinkPrefixList); prefix != "" {
			switch prefix {
			case DefaultThunderPrefix:
				b, err := base64.StdEncoding.DecodeString(strings.Replace(link, DefaultThunderPrefix, "", -1))
				if err != nil {
					log.Printf("Thunder url parse fail %+v\n", err)
					continue
				}
				r, _ := regexp.Compile("AA(.*)ZZ")
				link = r.FindStringSubmatch(string(b))[1]
				break
			}

			go func(downLink string) {
				ks.linkInfo(downLink)
			}(link)
		} else {
			log.Printf("Down link error,support only prefix with %s\n", strings.Join(DefaultDownLinkPrefixList, ","))
		}
	}
}

func (ks *KSDown) StartDownload() {
	// 一个一个文件过来
	ks.Close()
	for i := 0; i < ks.Goroutine; i++ {
		go func(tid int) {
			for fp := range ks.FilePart {

				// 部分下载完成时处理
				if err := fp.startPartDownload(int64(tid)); err == nil {
				}

			}
		}(i)
	}
	<-ks.Stop
	return
}

func (ks *KSDown) linkInfo(downLink string) error {
	fileName := path.Base(downLink)
	log.Printf("开始获取文件[%s]信息.......\n", fileName)
	resp, err := utils.HttpRequest(utils.HttpMethodGet, downLink, nil, map[string]string{
		"User-Agent": DefaultUserAgent,
	})
	if err != nil {
		log.Printf("获取文件[%s] 大小失败 %+v\n", fileName, err)
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Close  Response Err %+v\n", err)
		}
	}()

	fileSize, err := strconv.Atoi(resp.Header["Content-Length"][0])
	if err != nil {
		log.Printf("获取文件 [%s]  Content-Length 头部属性 失败 %+v\n", fileName, err)
		return err
	}
	festPath := DefaultDownPath + string(os.PathSeparator) + fileName
	//开始分块
	log.Printf("成功获取文件[%s]信息,文件大小：%d  开始分块\n", fileName, fileSize)
	blockList := ks.toBlock(fileSize)
	blockNum := len(blockList)
	for i, ps := range blockList {

		ks.FilePart <- &FilePart{
			Url:      downLink,
			BlockNum: blockNum,
			BlockId:  strconv.Itoa(i),
			FileSize: fileSize,
			FileName: fileName,
			Part:     ps,
			DestPath: festPath,
		}

	}
	return nil
}

func (ks *KSDown) toBlock(size int) []Part {

	avgPart := int(math.Floor(float64(size) / float64(ks.Goroutine)))
	block := []Part(nil)
	for i := 0; i < ks.Goroutine; i++ {
		part := i * avgPart
		stop := part + avgPart

		if i == (10 - 1) {
			stop = size
		}

		block = append(block, Part{
			Start: int64(part),
			Stop:  stop,
		})

	}
	return block
}

func (ks *KSDown) Close() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, DeadSignal...)
	go func() {
		log.Printf("收到终止信号 %+v ，退出下载\n", <-ch)
		ks.Stop <- true
		os.Exit(1)
	}()
}

func (fp *FilePart) startPartDownload(threadId int64) error {
	fmt.Printf("协程 %d 开始处理文件 %s Range= %d ~ %d \n", threadId, fp.FileName, fp.Part.Start, fp.Part.Stop)

	resp, err := utils.HttpRequest(utils.HttpMethodGet, fp.Url, nil, map[string]string{
		"User-Agent": DefaultUserAgent,
		"Range":      fmt.Sprintf("bytes=%d-%d", fp.Part.Start, fp.Part.Stop),
	})
	if err != nil {
		log.Printf("协程 %d 访问下载链接 %s 失败 : %+v\n", threadId, fp.Url, err)
		return err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("协程 %d 关闭下载链接 %s 失败 : %+v\n", threadId, fp.Url, err)
		}
	}()

	if resp.StatusCode == 206 {
		fmt.Printf("协程 %d 开始下载文件 %s  Range= %d ~ %d \n", threadId, fp.FileName, fp.Part.Start, fp.Part.Stop)
		var body []byte
		if body, err = ioutil.ReadAll(resp.Body); err != nil {
			log.Printf("协程 %d 读取下载链接 %s 内容Range= %d ~ %d 失败 : %+v\n", threadId, fp.Url, fp.Part.Start, fp.Part.Stop, err)
			return err
		}

		fp.filePoint, err = os.OpenFile(fp.DestPath, os.O_CREATE, os.ModePerm)
		if err != nil {
			log.Printf("打开目录文件%s失败，请检查\n", fp.DestPath)
			return err
		}
		defer func() {
			if err := fp.filePoint.Close(); err != nil {
				log.Printf("关闭文件%s失败：%+v\n", fp.DestPath, err)
			} else {
				log.Printf("解除当前协程 %d 对文件 %s 的打开句柄\n", threadId, fp.DestPath)
			}
		}()

		if _, err := fp.filePoint.WriteAt(body, fp.Part.Start); err != nil {
			log.Printf("协程 %d 写入链接 %s 内容Range= %d ~ %d失败 : %+v\n", threadId, fp.Url, fp.Part.Start, fp.Part.Stop, err)
			return err
		}

		fmt.Printf("协程 %d 下载文件 %s 部分 Range= %d ~ %d 完成！！！\n", threadId, fp.FileName, fp.Part.Start, fp.Part.Stop)
		return nil
	}
	return errors.New("不支持断点下载，返回状态码非206")
}

// 读取剪切板中的内容到字符串
func (ks *KSDown) listenClipboard() {

	go func() {
		hasReadMap := make(map[string]int64)
		for t := range time.NewTicker(1 * time.Second).C {
			content, err := clipboard.ReadAll()
			if err != nil {
				log.Printf("read clipboard error %+v\n", err)
				continue
			}
			content = strings.TrimSpace(content)
			if _, ok := hasReadMap[content]; ok {
				continue
			}
			log.Printf("has read clipboard content %s\n", content)
			ks.addTask(strings.Split(content, "\r\n"))
			hasReadMap[content] = t.Unix()
		}
	}()
}

var (
	url  string
	file string
)

func main() {
	var urlList []string
	flag.StringVar(&url, "u", "", "example ksdowm -u http://cc.com/dddd.mp4,http://cc.com/dddd.mp4,....")
	flag.StringVar(&file, "f", "", "example ksdowm -f /root/home/xxx")
	flag.IntVar(&DefaultGoroutineNum, "g", DefaultGoroutineNum, "example ksdowm -g 20")
	flag.Parse()

	if strings.TrimSpace(file) == "" && strings.TrimSpace(url) == "" {
		log.Printf("lose param -f ," +
			"\nexample ksdowm -u http://cc.com/dddd.mp4,http://cc.com/dddd.mp4,.... \n" +
			"or\nksdowm -f /root/home/urlfile\nurlfile format is :" +
			"\nhttp://cc.com/1.mp4\nhttp://cc.com/2.mp4\nhttp://cc.com/3.mp4\n....")
	}

	if strings.TrimSpace(file) != "" {
		b, err := ioutil.ReadFile(file)
		if err != nil {
			log.Printf("read file %s error : %+v\n", file, err)
		} else {
			urlList = strings.Split(string(b), "\r\n")
		}
	}
	if len(urlList) == 0 && url != "" {
		urlList = strings.Split(url, ",")
	}

	ks, err := NewKSDown(true)
	if err != nil {
		log.Fatalf("KSDown Step Error : %+v\n", err)
	}
	ks.listenClipboard()
	ks.addTask(urlList)
	ks.StartDownload()
}

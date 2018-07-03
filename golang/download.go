package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

var quit chan string = make(chan string)

func main() {
	body, err := Get_res("http://xxx.xxx.xxx")
	if err != nil {
		log.Fatal(err)
		return
	}
	// 下载页面
	r, _ := regexp.Compile(".*key.*")
	match := r.FindAll(body, -1)

	r, _ = regexp.Compile("\"http.*\"")
	match = r.FindAll(match[0], -1)

	tag := bytes.Trim(match[0], "\"")
	body, err = Get_res(string(tag))
	if err != nil {
		log.Fatal(err)
		return
	}

	// zip包地址
	r, _ = regexp.Compile(".*\\.zip")
	match = r.FindAll(body, -1)
	for i, j := range match {

		str := bytes.Split(j, []byte("\""))
		download_url := string(str[len(str)-1])
		fmt.Printf("%d\t%s\n", i, download_url)
		// 下载
		go Downloadfile(download_url)

	}

	for i, _ := range match {
		fmt.Printf("%d\t%s\n", i, <-quit)
	}

	// 等待用户响应
	fmt.Println("Press the Enter Key to terminate the console screen!")
	fmt.Scanln()
}

func Get_url(address string) (response *http.Response, err error) {
	client := &http.Client{}
	addr, err := url.Parse(address)
	if err != nil {
		log.Fatal(err)
		return
	}
	reqest, err := http.NewRequest("GET", addr.String(), nil)
	reqest.Header.Add("Host", addr.Host)
	reqest.Header.Add("Referer", addr.String())
	reqest.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:59.0) Gecko/20100101 Firefox/59.0")
	reqest.Header.Add("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	reqest.Header.Add("Accept-Encoding", "")
	reqest.Header.Add("Accept-Language", "zh-cn,zh;q=0.8,en-us;q=0.5,en;q=0.3")
	reqest.Header.Add("Connection", "keep-alive")
	if err != nil {
		log.Fatal(err)
		return
	}
	response, err = client.Do(reqest)
	if err != nil {
		log.Fatal(err)
		return
	}

	return response, nil
}

func Get_res(address string) (body []byte, err error) {

	response, err := Get_url(address)

	if response.StatusCode == 200 {
		body, err = ioutil.ReadAll(response.Body)
		if err != nil {
			log.Fatal(err)
			return
		}
	}

	response.Body.Close()
	return body, err
}

func Downloadfile(dl string) error {
	filename := strings.Split(dl, "/")[len(strings.Split(dl, "/"))-1]
	filepath := "all_map_packages"
	_, err := os.Stat(filepath)
	if err != nil {
		err := os.MkdirAll(filepath, 0711)
		if err != nil {
			log.Fatal(err)
			return err
		}
	}

	file := path.Join(filepath, filename)
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	response, err := Get_url(dl)

	if response.StatusCode == 200 {
		_, err = io.Copy(f, response.Body)
		if err != nil {
			log.Fatal(err)
			return err
		}
		quit <- filename
	}

	response.Body.Close()
	return nil
}

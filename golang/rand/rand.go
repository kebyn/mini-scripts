package main

import (
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"time"
)

// 生成位数
func main() {
	n, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Panicf("%v\n", err)
	}
	password := Randpassword(n)
	log.Printf("%v\n", password)
}

// 确认随机字符包含大小写字母、数字、特殊字符
func Randpassword(n int) (randstring string) {
	if n < 4 {
		log.Panicf("n must be greater than 3\n")
	}
	for {
		randstring = RandStringBytesMaskImprSrc(n)
		upper, _ := regexp.MatchString("[A-Z]", randstring)
		lower, _ := regexp.MatchString("[a-z]", randstring)
		number, _ := regexp.MatchString("[0-9]", randstring)
		other, _ := regexp.MatchString("[^0-9a-zA-Z]", randstring)
		if upper && lower && number && other {
			break
		}
	}
	return randstring
}

// 随机生成字符
func RandStringBytesMaskImprSrc(n int) string {
	const letterBytes = "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ()`~!@#$%^&*-+=|{}[]:;<>,.?/"
	src := rand.NewSource(time.Now().UnixNano())
	const (
		letterIdxBits = 6                    // 6 bits to represent a letter index
		letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
		letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
	)
	s := make([]byte, n)
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			s[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(s)
}

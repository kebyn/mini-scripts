package main

import (
	"flag"
	"log"
	"math/rand"
	"regexp"
	"strconv"
	"time"
)

const (
	alphanumericBytes = "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	specialBytes      = "()`~!@#$%^&*-+=|{}[]:;<>,.?/"
)

var (
	upperRegexp  = regexp.MustCompile(`[A-Z]`)
	lowerRegexp  = regexp.MustCompile(`[a-z]`)
	numberRegexp = regexp.MustCompile(`[0-9]`)
	otherRegexp  = regexp.MustCompile(`[^0-9a-zA-Z]`)
)

func main() {
	length := flag.Int("n", 8, "password length (must be >= 4)")
	useSpecial := flag.Bool("s", false, "include special characters")
	flag.Parse()

	n := *length
	if flag.NArg() > 0 {
		parsed, err := strconv.Atoi(flag.Arg(0))
		if err != nil {
			log.Fatalf("invalid password length %q: %v\n", flag.Arg(0), err)
		}
		n = parsed
	}

	password := Randpassword(n, *useSpecial)
	log.Printf("%s\n", password)
}

// Randpassword generates a random password of length n.
// When useSpecial is true, the generated password contains uppercase, lowercase, digits, and special characters.
func Randpassword(n int, useSpecial bool) (randstring string) {
	if n < 4 {
		log.Fatalf("n must be greater than 3\n")
	}
	charset := alphanumericBytes
	if useSpecial {
		charset += specialBytes
	}
	for {
		randstring = RandStringBytesMaskImprSrc(n, charset)
		upper := upperRegexp.MatchString(randstring)
		lower := lowerRegexp.MatchString(randstring)
		number := numberRegexp.MatchString(randstring)
		if !useSpecial {
			if upper && lower && number {
				break
			}
			continue
		}
		other := otherRegexp.MatchString(randstring)
		if upper && lower && number && other {
			break
		}
	}
	return randstring
}

func RandStringBytesMaskImprSrc(n int, letterBytes string) string {
	src := rand.NewSource(time.Now().UnixNano())
	const (
		letterIdxBits = 7                    // 7 bits to represent a letter index (covers up to 127 chars)
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

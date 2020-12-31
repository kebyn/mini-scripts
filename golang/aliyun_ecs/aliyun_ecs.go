package main

import (
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
)

func main() {
	var (
		ipaddress  string
		password   string
		instanceid string
		status     string
		err        error
	)
	releasetime, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Panicf("%v\n", err)
	}
	//  AccessKey
	ecsClient, err := ecs.NewClientWithAccessKey(
		"cn-shanghai",
		"xxx",
		"xxx")
	if err != nil {
		log.Panicf("%v\n", err)
	}
	// rand password
	password = Randpassword(8)
	instanceid, err = Createinstance(ecsClient, frontend, password)
	if err != nil {
		log.Panicf("%v\n", err)
	}
	instance := "[\"" + instanceid + "\"]"
	// status loop
Loop:
	for {
		status, ipaddress, err = Describeinstances(ecsClient, instance)
		if err != nil {
			log.Panicf("%v\n", err)
		} else if status == "Stopped" {
			ipaddress, err = Allocatepublicipaddress(ecsClient, instanceid)
			if err != nil {
				log.Panicf("%v\n", err)
			}
			log.Printf("ipaddress :\t %s \n", ipaddress)
			log.Printf("instance  started !!!\n")
			err = Startinstance(ecsClient, instanceid)
			if err != nil {
				log.Panicf("%v\n", err)
			}
		} else if status == "Running" {
			log.Printf("ip :\t %s \t password :\t %s \n", ipaddress, password)
			err = Autorelease(ecsClient, instanceid, releasetime)
			if err != nil {
				log.Panicf("%v\n", err)
			}
			break Loop
		} else {
			log.Printf("instance status :\t %s \n", status)
			// sleep
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// create instance
func Createinstance(ecsClient *ecs.Client, hostname, password string) (instanceid string, err error) {
	request := ecs.CreateCreateInstanceRequest()
	request.ImageId = ""
	request.InstanceType = ""
	request.SecurityGroupId = ""
	request.InternetChargeType = "PayByTraffic"
	request.InternetMaxBandwidthIn = "200"
	request.InternetMaxBandwidthOut = "100"
	request.IoOptimized = "optimized"
	request.VSwitchId = ""
	request.InstanceChargeType = "PostPaid"
	request.HostName = hostname
	request.Password = password
	request.InstanceName = hostname
	request.SecurityEnhancementStrategy = "Active"
	//request.ClientToken = utils.GetUUIDV4()
	response, err := ecsClient.CreateInstance(request)
	if err != nil {
		log.Panicf("%v\n", err)
		return
	}
	log.Printf("success(%d)! requestId = %s\t instanceId = %s\n", response.GetHttpStatus(), response.RequestId, response.InstanceId)
	return response.InstanceId, nil
}

// allocate public ip instance
func Allocatepublicipaddress(ecsClient *ecs.Client, instanceid string) (ipaddress string, err error) {
	request := ecs.CreateAllocatePublicIpAddressRequest()
	request.InstanceId = instanceid
	response, err := ecsClient.AllocatePublicIpAddress(request)
	if err != nil {
		log.Panicf("%v\n", err)
		return
	}
	ipaddress = response.IpAddress
	log.Printf("success(%d)! IpAddress = %s\n", response.GetHttpStatus(), ipaddress)
	return ipaddress, nil
}

// starting instance
func Startinstance(ecsClient *ecs.Client, instanceid string) (err error) {
	request := ecs.CreateStartInstanceRequest()
	request.InstanceId = instanceid
	response, err := ecsClient.StartInstance(request)
	if err != nil {
		log.Panicf("%v\n", err)
		return
	}
	log.Printf("success(%d)! requestId = %s\n", response.GetHttpStatus(), response.RequestId)
	return
}

// auto release instance
func Autorelease(ecsClient *ecs.Client, instanceid string, duration int) (err error) {
	request := ecs.CreateModifyInstanceAutoReleaseTimeRequest()
	request.InstanceId = instanceid
	// auto release time
	request.AutoReleaseTime = time.Now().Add(time.Hour * time.Duration(duration)).UTC().Format("2006-01-02T15:04:05Z")
	response, err := ecsClient.ModifyInstanceAutoReleaseTime(request)
	if err != nil {
		log.Panicf("%v\n", err)
		return
	}
	log.Printf("success(%d)! requestId = %s\n", response.GetHttpStatus(), response.RequestId)
	return
}

// describe instance status/ip
func Describeinstances(ecsClient *ecs.Client, instanceid string) (status, ipaddress string, err error) {
	request := ecs.CreateDescribeInstancesRequest()
	request.InstanceIds = instanceid
	response, err := ecsClient.DescribeInstances(request)
	if err != nil {
		log.Panicf("%v\n", err)
		return
	}
	// get instance status
	status = response.Instances.Instance[0].Status
	log.Printf("success(%d)! status = %s\n", response.GetHttpStatus(), status)
	if status == "Running" {
		// get instance ip
		ipaddress = response.Instances.Instance[0].PublicIpAddress.IpAddress[0]
		log.Printf("success(%d)! ip = %s\n", response.GetHttpStatus(), ipaddress)
		return status, ipaddress, nil
	}
	return response.Instances.Instance[0].Status, ipaddress, nil
}

// range password
func Randpassword(n int) (randstring string) {
	if n < 8 && n > 30 {
		log.Panicf("n must be greater than 8 and less than 30 \n")
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

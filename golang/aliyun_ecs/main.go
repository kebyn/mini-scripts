package main

import (
	"flag"
	"log"
	"math/rand"
	"os"
	"regexp"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
)

var (
	upperRegexp  = regexp.MustCompile(`[A-Z]`)
	lowerRegexp  = regexp.MustCompile(`[a-z]`)
	numberRegexp = regexp.MustCompile(`[0-9]`)
	otherRegexp  = regexp.MustCompile(`[^0-9a-zA-Z]`)
)

func main() {
	var (
		region      string
		hostname    string
		releasetime int64
		ipaddress   string
		password    string
		instanceid  string
		status      string
		err         error
	)
	flag.StringVar(&region, "region", "cn-shanghai", "Alibaba Cloud region ID")
	flag.StringVar(&hostname, "hostname", "example", "instance hostname")
	flag.Int64Var(&releasetime, "releasetime", 1, "auto release time in hours")
	flag.Parse()

	accessKeyID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		log.Fatal("environment variables ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required")
	}

	ecsClient, err := ecs.NewClientWithAccessKey(
		region,
		accessKeyID,
		accessKeySecret,
	)
	if err != nil {
		log.Panicf("%v", err)
	}
	// rand password
	password = Randpassword(8)
	instanceid, err = Createinstance(ecsClient, hostname, password)
	if err != nil {
		log.Panicf("%v", err)
	}
	instance := "[\"" + instanceid + "\"]"
	// status loop
	for {
		status, ipaddress, err = Describeinstances(ecsClient, instance)
		if err != nil {
			log.Panicf("%v", err)
		} else if status == "Stopped" {
			ipaddress, err = Allocatepublicipaddress(ecsClient, instanceid)
			if err != nil {
				log.Panicf("%v", err)
			}
			log.Printf("ipaddress :\t %s ", ipaddress)
			log.Printf("instance  started !!!")
			err = Startinstance(ecsClient, instanceid)
			if err != nil {
				log.Panicf("%v", err)
			}
		} else if status == "Running" {
			log.Printf("ip :\t %s \t password :\t %s ", ipaddress, password)
			err = Autorelease(ecsClient, instanceid, releasetime)
			if err != nil {
				log.Panicf("%v", err)
			}
			break
		} else {
			log.Printf("instance status :\t %s ", status)
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// Createinstance creates a spot ECS instance.
func Createinstance(ecsClient *ecs.Client, hostname, password string) (instanceid string, err error) {
	request := ecs.CreateCreateInstanceRequest()
	// https://help.aliyun.com/zh/ecs/user-guide/overview-45
	request.ImageFamily = "acs:almalinux_9_2_x64"
	// https://help.aliyun.com/zh/ecs/user-guide/overview-of-instance-families?spm=a2c4g.11186623.0.i23#g8a
	request.InstanceType = "ecs.u1-c1m1.large"
	request.SecurityGroupId = os.Getenv("ALIBABA_CLOUD_SECURITY_GROUP_ID")
	request.InternetChargeType = "PayByTraffic"
	request.InternetMaxBandwidthIn = "100"
	request.InternetMaxBandwidthOut = "100"
	request.IoOptimized = "optimized"
	request.VSwitchId = os.Getenv("ALIBABA_CLOUD_VSWITCH_ID")
	request.InstanceChargeType = "PostPaid"
	request.SpotStrategy = "SpotWithPriceLimit"
	request.SpotPriceLimit = requests.NewFloat(0.98)
	request.HostName = hostname
	request.Password = password
	request.InstanceName = hostname
	request.SecurityEnhancementStrategy = "Deactive"
	response, err := ecsClient.CreateInstance(request)
	if err != nil {
		log.Panicf("%v", err)
		return
	}
	log.Printf("success(%d)! requestId = %s\t instanceId = %s", response.GetHttpStatus(), response.RequestId, response.InstanceId)
	return response.InstanceId, nil
}

// Allocatepublicipaddress allocates a public IP for the instance.
func Allocatepublicipaddress(ecsClient *ecs.Client, instanceid string) (ipaddress string, err error) {
	request := ecs.CreateAllocatePublicIpAddressRequest()
	request.InstanceId = instanceid
	response, err := ecsClient.AllocatePublicIpAddress(request)
	if err != nil {
		log.Panicf("%v", err)
		return
	}
	ipaddress = response.IpAddress
	log.Printf("success(%d)! IpAddress = %s", response.GetHttpStatus(), ipaddress)
	return ipaddress, nil
}

// Startinstance starts the ECS instance.
func Startinstance(ecsClient *ecs.Client, instanceid string) (err error) {
	request := ecs.CreateStartInstanceRequest()
	request.InstanceId = instanceid
	response, err := ecsClient.StartInstance(request)
	if err != nil {
		log.Panicf("%v", err)
		return
	}
	log.Printf("success(%d)! requestId = %s", response.GetHttpStatus(), response.RequestId)
	return
}

// Autorelease sets the auto release time of the ECS instance.
func Autorelease(ecsClient *ecs.Client, instanceid string, duration int64) (err error) {
	request := ecs.CreateModifyInstanceAutoReleaseTimeRequest()
	request.InstanceId = instanceid
	request.AutoReleaseTime = time.Now().Add(time.Hour * time.Duration(duration)).UTC().Format("2006-01-02T15:04:05Z")
	response, err := ecsClient.ModifyInstanceAutoReleaseTime(request)
	if err != nil {
		log.Panicf("%v", err)
		return
	}
	log.Printf("success(%d)! requestId = %s", response.GetHttpStatus(), response.RequestId)
	return
}

// Describeinstances queries instance status and public IP.
func Describeinstances(ecsClient *ecs.Client, instanceid string) (status, ipaddress string, err error) {
	request := ecs.CreateDescribeInstancesRequest()
	request.InstanceIds = instanceid
	response, err := ecsClient.DescribeInstances(request)
	if err != nil {
		log.Panicf("%v", err)
		return
	}
	if len(response.Instances.Instance) == 0 {
		return "Pending", "", nil
	}
	inst := response.Instances.Instance[0]
	status = inst.Status
	log.Printf("success(%d)! status = %s", response.GetHttpStatus(), status)
	if status == "Running" && len(inst.PublicIpAddress.IpAddress) > 0 {
		ipaddress = inst.PublicIpAddress.IpAddress[0]
		log.Printf("success(%d)! ip = %s", response.GetHttpStatus(), ipaddress)
		return status, ipaddress, nil
	}
	return status, ipaddress, nil
}

// Randpassword generates a random password of length n (8 <= n <= 30) containing upper, lower, digit, and special chars.
func Randpassword(n int) (randstring string) {
	if n < 8 || n > 30 {
		log.Panicf("n must be between 8 and 30")
	}
	for {
		randstring = RandStringBytesMaskImprSrc(n)
		if upperRegexp.MatchString(randstring) &&
			lowerRegexp.MatchString(randstring) &&
			numberRegexp.MatchString(randstring) &&
			otherRegexp.MatchString(randstring) {
			break
		}
	}
	return randstring
}

func RandStringBytesMaskImprSrc(n int) string {
	const letterBytes = "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ()`~!@#$%^&*-+=|{}[]:;<>,.?/"
	src := rand.NewSource(time.Now().UnixNano())
	const (
		letterIdxBits = 7                    // 7 bits to represent a letter index (up to 127)
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

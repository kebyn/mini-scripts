package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/models"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
)

var (
	upperRegexp  = regexp.MustCompile(`[A-Z]`)
	lowerRegexp  = regexp.MustCompile(`[a-z]`)
	numberRegexp = regexp.MustCompile(`[0-9]`)
	otherRegexp  = regexp.MustCompile(`[^0-9a-zA-Z]`)
)

func main() {
	var (
		provider    string
		region      string
		hostname    string
		releasetime int64
	)
	flag.StringVar(&provider, "provider", "aliyun", "cloud provider: aliyun or tencent")
	flag.StringVar(&region, "region", "", "cloud region ID (default: cn-shanghai for aliyun, ap-shanghai for tencent)")
	flag.StringVar(&hostname, "hostname", "example", "instance hostname")
	flag.Int64Var(&releasetime, "releasetime", 1, "auto release time in hours")
	flag.Parse()

	provider = strings.ToLower(strings.TrimSpace(provider))
	password := Randpassword(12)

	switch provider {
	case "aliyun":
		if region == "" {
			region = "cn-shanghai"
		}
		runAliyun(region, hostname, password, releasetime)
	case "tencent":
		if region == "" {
			region = "ap-shanghai"
		}
		runTencent(region, hostname, password, releasetime)
	default:
		log.Fatalf("unsupported provider %q: must be 'aliyun' or 'tencent'", provider)
	}
}

func runAliyun(region, hostname, password string, releasetime int64) {
	accessKeyID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		log.Fatal("environment variables ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required")
	}

	ecsClient, err := NewAliyunClient(region, accessKeyID, accessKeySecret)
	if err != nil {
		log.Panicf("%v", err)
	}

	instanceid, err := Createinstance(ecsClient, region, hostname, password)
	if err != nil {
		log.Panicf("%v", err)
	}
	instance := fmt.Sprintf("[\"%s\"]", instanceid)

	for {
		status, ipaddress, err := Describeinstances(ecsClient, region, instance)
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
			err = Autorelease(ecsClient, region, instanceid, releasetime)
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

// NewAliyunClient initializes an Alibaba Cloud ECS V2 SDK client.
func NewAliyunClient(region, accessKeyID, accessKeySecret string) (*ecs.Client, error) {
	config := &openapi.Config{
		AccessKeyId:     tea.String(accessKeyID),
		AccessKeySecret: tea.String(accessKeySecret),
		RegionId:        tea.String(region),
	}
	return ecs.NewClient(config)
}

// Createinstance creates a spot ECS instance on Alibaba Cloud.
func Createinstance(ecsClient *ecs.Client, region, hostname, password string) (instanceid string, err error) {
	request := &ecs.CreateInstanceRequest{
		RegionId:                    tea.String(region),
		ImageFamily:                 tea.String("acs:almalinux_9_2_x64"),
		InstanceType:                tea.String("ecs.u1-c1m1.large"),
		SecurityGroupId:             tea.String(os.Getenv("ALIBABA_CLOUD_SECURITY_GROUP_ID")),
		InternetChargeType:          tea.String("PayByTraffic"),
		InternetMaxBandwidthIn:      tea.Int32(100),
		InternetMaxBandwidthOut:     tea.Int32(100),
		IoOptimized:                 tea.String("optimized"),
		VSwitchId:                   tea.String(os.Getenv("ALIBABA_CLOUD_VSWITCH_ID")),
		InstanceChargeType:          tea.String("PostPaid"),
		SpotStrategy:                tea.String("SpotWithPriceLimit"),
		SpotPriceLimit:              tea.Float32(0.98),
		HostName:                    tea.String(hostname),
		Password:                    tea.String(password),
		InstanceName:                tea.String(hostname),
		SecurityEnhancementStrategy: tea.String("Deactive"),
	}
	response, err := ecsClient.CreateInstance(request)
	if err != nil {
		return "", err
	}
	instanceid = tea.StringValue(response.Body.InstanceId)
	log.Printf("success(%d)! requestId = %s\t instanceId = %s",
		tea.Int32Value(response.StatusCode),
		tea.StringValue(response.Body.RequestId),
		instanceid,
	)
	return instanceid, nil
}

// Allocatepublicipaddress allocates a public IP for the Alibaba Cloud ECS instance.
func Allocatepublicipaddress(ecsClient *ecs.Client, instanceid string) (ipaddress string, err error) {
	request := &ecs.AllocatePublicIpAddressRequest{
		InstanceId: tea.String(instanceid),
	}
	response, err := ecsClient.AllocatePublicIpAddress(request)
	if err != nil {
		return "", err
	}
	ipaddress = tea.StringValue(response.Body.IpAddress)
	log.Printf("success(%d)! IpAddress = %s", tea.Int32Value(response.StatusCode), ipaddress)
	return ipaddress, nil
}

// Startinstance starts the Alibaba Cloud ECS instance.
func Startinstance(ecsClient *ecs.Client, instanceid string) (err error) {
	request := &ecs.StartInstanceRequest{
		InstanceId: tea.String(instanceid),
	}
	response, err := ecsClient.StartInstance(request)
	if err != nil {
		return err
	}
	log.Printf("success(%d)! requestId = %s",
		tea.Int32Value(response.StatusCode),
		tea.StringValue(response.Body.RequestId),
	)
	return nil
}

// Autorelease sets the auto release time of the Alibaba Cloud ECS instance.
func Autorelease(ecsClient *ecs.Client, region, instanceid string, duration int64) (err error) {
	releaseTime := time.Now().Add(time.Hour * time.Duration(duration)).UTC().Format("2006-01-02T15:04:05Z")
	request := &ecs.ModifyInstanceAutoReleaseTimeRequest{
		RegionId:        tea.String(region),
		InstanceId:      tea.String(instanceid),
		AutoReleaseTime: tea.String(releaseTime),
	}
	response, err := ecsClient.ModifyInstanceAutoReleaseTime(request)
	if err != nil {
		return err
	}
	log.Printf("success(%d)! requestId = %s",
		tea.Int32Value(response.StatusCode),
		tea.StringValue(response.Body.RequestId),
	)
	return nil
}

// Describeinstances queries Alibaba Cloud ECS instance status and public IP.
func Describeinstances(ecsClient *ecs.Client, region, instanceid string) (status, ipaddress string, err error) {
	request := &ecs.DescribeInstancesRequest{
		RegionId:    tea.String(region),
		InstanceIds: tea.String(instanceid),
	}
	response, err := ecsClient.DescribeInstances(request)
	if err != nil {
		return "", "", err
	}
	if response.Body == nil || response.Body.Instances == nil || len(response.Body.Instances.Instance) == 0 {
		return "Pending", "", nil
	}
	inst := response.Body.Instances.Instance[0]
	status = tea.StringValue(inst.Status)
	statusCode := tea.Int32Value(response.StatusCode)
	log.Printf("success(%d)! status = %s", statusCode, status)
	if status == "Running" && inst.PublicIpAddress != nil && len(inst.PublicIpAddress.IpAddress) > 0 {
		ipaddress = tea.StringValue(inst.PublicIpAddress.IpAddress[0])
		log.Printf("success(%d)! ip = %s", statusCode, ipaddress)
		return status, ipaddress, nil
	}
	return status, ipaddress, nil
}

func runTencent(region, hostname, password string, releasetime int64) {
	secretID := os.Getenv("TENCENTCLOUD_SECRET_ID")
	secretKey := os.Getenv("TENCENTCLOUD_SECRET_KEY")
	zone := os.Getenv("TENCENTCLOUD_ZONE")
	if secretID == "" || secretKey == "" || zone == "" {
		log.Fatal("environment variables TENCENTCLOUD_SECRET_ID, TENCENTCLOUD_SECRET_KEY, and TENCENTCLOUD_ZONE are required")
	}

	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cvmClient, err := cvm.NewClient(credential, region, cpf)
	if err != nil {
		log.Panicf("%v", err)
	}

	instanceType := os.Getenv("TENCENTCLOUD_INSTANCE_TYPE")
	if instanceType == "" {
		instanceType = "SA2.MEDIUM2"
	}

	imageID := os.Getenv("TENCENTCLOUD_IMAGE_ID")
	if imageID == "" {
		imageID, err = ResolveTencentPublicImage(cvmClient, instanceType)
		if err != nil {
			log.Panicf("%v", err)
		}
	}

	instanceID, err := CreateTencentSpotInstance(cvmClient, zone, instanceType, imageID, hostname, password, releasetime)
	if err != nil {
		log.Panicf("%v", err)
	}

	for {
		status, ipaddress, err := DescribeTencentInstance(cvmClient, instanceID)
		if err != nil {
			log.Panicf("%v", err)
		}
		if status == "RUNNING" && ipaddress != "" {
			log.Printf("ip :\t %s \t password :\t %s ", ipaddress, password)
			break
		}
		log.Printf("instance status :\t %s ", status)
		time.Sleep(1000 * time.Millisecond)
	}
}

// ResolveTencentPublicImage queries a supported public Linux image (preferring AlmaLinux) for the instance type.
func ResolveTencentPublicImage(cvmClient *cvm.Client, instanceType string) (string, error) {
	request := cvm.NewDescribeImagesRequest()
	request.InstanceType = common.StringPtr(instanceType)
	request.Filters = []*cvm.Filter{
		{
			Name:   common.StringPtr("image-type"),
			Values: []*string{common.StringPtr("PUBLIC_IMAGE")},
		},
		{
			Name:   common.StringPtr("platform"),
			Values: []*string{common.StringPtr("AlmaLinux")},
		},
	}
	request.Limit = common.Uint64Ptr(1)

	response, err := cvmClient.DescribeImages(request)
	if err != nil {
		return "", err
	}
	if response.Response != nil && len(response.Response.ImageSet) > 0 && response.Response.ImageSet[0].ImageId != nil {
		imageID := *response.Response.ImageSet[0].ImageId
		log.Printf("resolved Tencent Cloud public image: %s", imageID)
		return imageID, nil
	}

	// Fallback to any public image supported by the instance type
	fallbackReq := cvm.NewDescribeImagesRequest()
	fallbackReq.InstanceType = common.StringPtr(instanceType)
	fallbackReq.Filters = []*cvm.Filter{
		{
			Name:   common.StringPtr("image-type"),
			Values: []*string{common.StringPtr("PUBLIC_IMAGE")},
		},
	}
	fallbackReq.Limit = common.Uint64Ptr(1)
	fallbackResp, err := cvmClient.DescribeImages(fallbackReq)
	if err != nil {
		return "", err
	}
	if fallbackResp.Response != nil && len(fallbackResp.Response.ImageSet) > 0 && fallbackResp.Response.ImageSet[0].ImageId != nil {
		imageID := *fallbackResp.Response.ImageSet[0].ImageId
		log.Printf("resolved Tencent Cloud fallback public image: %s", imageID)
		return imageID, nil
	}
	return "", fmt.Errorf("no available public image found for instance type %s; please set TENCENTCLOUD_IMAGE_ID", instanceType)
}

// CreateTencentSpotInstance creates a SPOTPAID CVM instance with public IP and ActionTimer auto-termination.
func CreateTencentSpotInstance(cvmClient *cvm.Client, zone, instanceType, imageID, hostname, password string, releasetime int64) (string, error) {
	request := cvm.NewRunInstancesRequest()
	request.InstanceChargeType = common.StringPtr("SPOTPAID")
	request.Placement = &cvm.Placement{
		Zone: common.StringPtr(zone),
	}
	request.InstanceType = common.StringPtr(instanceType)
	request.ImageId = common.StringPtr(imageID)
	request.InstanceCount = common.Int64Ptr(1)
	request.InstanceName = common.StringPtr(hostname)
	request.HostName = common.StringPtr(hostname)

	request.InstanceMarketOptions = &cvm.InstanceMarketOptionsRequest{
		MarketType: common.StringPtr("spot"),
		SpotOptions: &cvm.SpotMarketOptions{
			MaxPrice:         common.StringPtr("0.98"),
			SpotInstanceType: common.StringPtr("one-time"),
		},
	}

	request.InternetAccessible = &cvm.InternetAccessible{
		InternetChargeType:      common.StringPtr("TRAFFIC_POSTPAID_BY_HOUR"),
		InternetMaxBandwidthOut: common.Int64Ptr(100),
		PublicIpAssigned:        common.BoolPtr(true),
	}

	request.LoginSettings = &cvm.LoginSettings{
		Password: common.StringPtr(password),
	}

	vpcID := os.Getenv("TENCENTCLOUD_VPC_ID")
	subnetID := os.Getenv("TENCENTCLOUD_SUBNET_ID")
	if vpcID == "" {
		vpcID = "DEFAULT"
	}
	if subnetID == "" {
		subnetID = "DEFAULT"
	}
	request.VirtualPrivateCloud = &cvm.VirtualPrivateCloud{
		VpcId:    common.StringPtr(vpcID),
		SubnetId: common.StringPtr(subnetID),
	}

	if sgID := os.Getenv("TENCENTCLOUD_SECURITY_GROUP_ID"); sgID != "" {
		request.SecurityGroupIds = []*string{common.StringPtr(sgID)}
	}

	if releasetime > 0 {
		actionTime := time.Now().Add(time.Hour * time.Duration(releasetime)).UTC().Format("2006-01-02T15:04:05Z")
		request.ActionTimer = &cvm.ActionTimer{
			TimerAction: common.StringPtr("TerminateInstances"),
			ActionTime:  common.StringPtr(actionTime),
			Externals: &cvm.Externals{
				ReleaseAddress: common.BoolPtr(true),
			},
		}
	}

	response, err := cvmClient.RunInstances(request)
	if err != nil {
		return "", err
	}
	if response.Response == nil || len(response.Response.InstanceIdSet) == 0 || response.Response.InstanceIdSet[0] == nil {
		return "", fmt.Errorf("empty InstanceIdSet in RunInstances response")
	}
	instanceID := *response.Response.InstanceIdSet[0]
	requestID := ""
	if response.Response.RequestId != nil {
		requestID = *response.Response.RequestId
	}
	log.Printf("success! requestId = %s\t instanceId = %s", requestID, instanceID)
	return instanceID, nil
}

// DescribeTencentInstance queries the status and public IP of a Tencent Cloud CVM instance.
func DescribeTencentInstance(cvmClient *cvm.Client, instanceID string) (status, ipaddress string, err error) {
	request := cvm.NewDescribeInstancesRequest()
	request.InstanceIds = []*string{common.StringPtr(instanceID)}

	response, err := cvmClient.DescribeInstances(request)
	if err != nil {
		return "", "", err
	}
	if response.Response == nil || len(response.Response.InstanceSet) == 0 {
		return "PENDING", "", nil
	}
	inst := response.Response.InstanceSet[0]
	if inst.InstanceState != nil {
		status = *inst.InstanceState
	}
	if len(inst.PublicIpAddresses) > 0 && inst.PublicIpAddresses[0] != nil {
		ipaddress = *inst.PublicIpAddresses[0]
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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/models"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
)

// releaseSafetyBuffer is subtracted from the target billing cycle boundary
// so the VM is released slightly before entering the next hourly billing cycle.
const releaseSafetyBuffer = 2 * time.Minute

var (
	upperRegexp  = regexp.MustCompile(`[A-Z]`)
	lowerRegexp  = regexp.MustCompile(`[a-z]`)
	numberRegexp = regexp.MustCompile(`[0-9]`)
	otherRegexp  = regexp.MustCompile(`[^0-9a-zA-Z]`)
)

// InstanceSummary holds normalized instance metadata across cloud providers.
type InstanceSummary struct {
	ID              string
	Name            string
	Status          string
	PublicIP        string
	PrivateIP       string
	InstanceType    string
	CreatedTime     string
	AutoReleaseTime string
	ActionTimerIDs  []string
}

func main() {
	var (
		provider    string
		action      string
		region      string
		idFlag      string
		hostname    string
		releasetime int64
		maxprice    float64
	)
	flag.StringVar(&provider, "provider", "aliyun", "cloud provider: aliyun or tencent")
	flag.StringVar(&action, "action", "create", "action to perform: create, list, delete, or extend")
	flag.StringVar(&region, "region", "", "cloud region ID (default: cn-shanghai for aliyun, ap-shanghai for tencent)")
	flag.StringVar(&idFlag, "id", "", "instance ID(s), comma-separated (required for delete/extend, optional for list)")
	flag.StringVar(&hostname, "hostname", "example", "instance hostname (for create)")
	flag.Int64Var(&releasetime, "releasetime", 1, "auto release duration in hours (for create/extend)")
	flag.Float64Var(&maxprice, "maxprice", 0.98, "max spot price per hour (for create)")
	flag.Parse()

	provider = strings.ToLower(strings.TrimSpace(provider))
	action = strings.ToLower(strings.TrimSpace(action))
	ids := parseInstanceIDs(idFlag)

	switch action {
	case "create":
		if maxprice <= 0 {
			log.Fatalf("invalid -maxprice %.4f: must be greater than 0", maxprice)
		}
		if releasetime <= 0 {
			log.Fatalf("invalid -releasetime %d: must be greater than 0", releasetime)
		}
	case "list":
		// ids is optional filter
	case "delete":
		if len(ids) == 0 {
			log.Fatal("-id is required for -action delete")
		}
	case "extend":
		if len(ids) == 0 {
			log.Fatal("-id is required for -action extend")
		}
		if releasetime <= 0 {
			log.Fatalf("invalid -releasetime %d: must be greater than 0 for -action extend", releasetime)
		}
	default:
		log.Fatalf("unsupported action %q: must be 'create', 'list', 'delete', or 'extend'", action)
	}

	switch provider {
	case "aliyun":
		if region == "" {
			region = "cn-shanghai"
		}
		dispatchAliyun(action, region, hostname, releasetime, maxprice, ids)
	case "tencent":
		if region == "" {
			region = "ap-shanghai"
		}
		dispatchTencent(action, region, hostname, releasetime, maxprice, ids)
	default:
		log.Fatalf("unsupported provider %q: must be 'aliyun' or 'tencent'", provider)
	}
}

func parseInstanceIDs(raw string) []string {
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

// parseCloudTime parses ISO8601 UTC timestamps returned by Aliyun ECS or Tencent CVM.
func parseCloudTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04Z",
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// calcCreateReleaseTime computes the auto-release timestamp from the VM creation time
// plus releasetimeHours minus releaseSafetyBuffer to avoid exceeding the billing cycle.
func calcCreateReleaseTime(creationTimeStr string, fallbackStart time.Time, releasetimeHours int64) time.Time {
	baseTime := fallbackStart.UTC()
	if parsed, ok := parseCloudTime(creationTimeStr); ok {
		baseTime = parsed
	}
	return baseTime.Add(time.Duration(releasetimeHours)*time.Hour - releaseSafetyBuffer).UTC()
}

// calcExtendReleaseTime extends the current scheduled release time by extendHours.
// If no future release time is set, it aligns to the current hourly billing cycle from creationTime
// and extends by extendHours minus releaseSafetyBuffer.
func calcExtendReleaseTime(currentReleaseTimeStr, creationTimeStr string, now time.Time, extendHours int64) time.Time {
	now = now.UTC()
	if curRelease, ok := parseCloudTime(currentReleaseTimeStr); ok && curRelease.After(now) {
		return curRelease.Add(time.Duration(extendHours) * time.Hour).UTC()
	}
	if created, ok := parseCloudTime(creationTimeStr); ok && !now.Before(created) {
		elapsed := now.Sub(created)
		currentCycles := int64(elapsed/time.Hour) + 1
		target := created.Add(time.Duration(currentCycles+extendHours)*time.Hour - releaseSafetyBuffer).UTC()
		if target.After(now) {
			return target
		}
	}
	return now.Add(time.Duration(extendHours)*time.Hour - releaseSafetyBuffer).UTC()
}

func printInstanceTable(instances []InstanceSummary) {
	if len(instances) == 0 {
		fmt.Println("No instances found.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INSTANCE_ID\tNAME\tSTATUS\tPUBLIC_IP\tPRIVATE_IP\tTYPE\tCREATED_TIME\tAUTO_RELEASE_TIME")
	for _, inst := range instances {
		pubIP := inst.PublicIP
		if pubIP == "" {
			pubIP = "-"
		}
		privIP := inst.PrivateIP
		if privIP == "" {
			privIP = "-"
		}
		autoRelease := inst.AutoReleaseTime
		if autoRelease == "" {
			autoRelease = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			inst.ID,
			inst.Name,
			inst.Status,
			pubIP,
			privIP,
			inst.InstanceType,
			inst.CreatedTime,
			autoRelease,
		)
	}
	_ = w.Flush()
}

func dispatchAliyun(action, region, hostname string, releasetime int64, maxprice float64, ids []string) {
	accessKeyID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		log.Fatal("environment variables ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required")
	}

	ecsClient, err := NewAliyunClient(region, accessKeyID, accessKeySecret)
	if err != nil {
		log.Panicf("%v", err)
	}

	switch action {
	case "create":
		password := Randpassword(12)
		runAliyun(ecsClient, region, hostname, password, releasetime, maxprice)
	case "list":
		instances, err := FetchAliyunInstances(ecsClient, region, ids)
		if err != nil {
			log.Panicf("%v", err)
		}
		printInstanceTable(instances)
	case "delete":
		if err := DeleteAliyunInstances(ecsClient, ids); err != nil {
			log.Panicf("%v", err)
		}
	case "extend":
		if err := ExtendAliyunInstances(ecsClient, region, ids, releasetime); err != nil {
			log.Panicf("%v", err)
		}
	}
}

func runAliyun(ecsClient *ecs.Client, region, hostname, password string, releasetime int64, maxprice float64) {
	if os.Getenv("ALIBABA_CLOUD_SECURITY_GROUP_ID") == "" || os.Getenv("ALIBABA_CLOUD_VSWITCH_ID") == "" {
		log.Fatal("environment variables ALIBABA_CLOUD_SECURITY_GROUP_ID and ALIBABA_CLOUD_VSWITCH_ID are required for creating an Aliyun instance")
	}

	createStartTime := time.Now().UTC()
	instanceid, err := Createinstance(ecsClient, region, hostname, password, maxprice)
	if err != nil {
		log.Panicf("%v", err)
	}
	instance := fmt.Sprintf("[\"%s\"]", instanceid)
	started := false

	for {
		status, ipaddress, creationTime, err := DescribeinstanceDetail(ecsClient, region, instance)
		if err != nil {
			log.Panicf("%v", err)
		} else if status == "Stopped" {
			if ipaddress == "" {
				ipaddress, err = Allocatepublicipaddress(ecsClient, instanceid)
				if err != nil {
					log.Panicf("%v", err)
				}
				log.Printf("allocated ipaddress :\t %s ", ipaddress)
			} else {
				log.Printf("using existing ipaddress :\t %s ", ipaddress)
			}
			if !started {
				err = Startinstance(ecsClient, instanceid)
				if err != nil {
					log.Panicf("%v", err)
				}
				started = true
				log.Printf("instance  started !!!")
			}
			time.Sleep(1000 * time.Millisecond)
		} else if status == "Running" {
			if ipaddress == "" {
				ipaddress, err = Allocatepublicipaddress(ecsClient, instanceid)
				if err != nil {
					log.Panicf("%v", err)
				}
				log.Printf("allocated ipaddress :\t %s ", ipaddress)
			} else {
				log.Printf("using existing ipaddress :\t %s ", ipaddress)
			}
			log.Printf("ip :\t %s \t password :\t %s ", ipaddress, password)
			releaseAt := calcCreateReleaseTime(creationTime, createStartTime, releasetime)
			err = AutoreleaseAt(ecsClient, region, instanceid, releaseAt)
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
func Createinstance(ecsClient *ecs.Client, region, hostname, password string, maxprice float64) (instanceid string, err error) {
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
		SpotPriceLimit:              tea.Float32(float32(maxprice)),
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

// Autorelease sets the auto release time of the Alibaba Cloud ECS instance relative to now.
func Autorelease(ecsClient *ecs.Client, region, instanceid string, duration int64) (err error) {
	releaseAt := calcCreateReleaseTime("", time.Now().UTC(), duration)
	return AutoreleaseAt(ecsClient, region, instanceid, releaseAt)
}

// AutoreleaseAt sets the exact auto release timestamp of the Alibaba Cloud ECS instance.
func AutoreleaseAt(ecsClient *ecs.Client, region, instanceid string, releaseAt time.Time) (err error) {
	releaseTime := releaseAt.UTC().Format("2006-01-02T15:04:05Z")
	request := &ecs.ModifyInstanceAutoReleaseTimeRequest{
		RegionId:        tea.String(region),
		InstanceId:      tea.String(instanceid),
		AutoReleaseTime: tea.String(releaseTime),
	}
	response, err := ecsClient.ModifyInstanceAutoReleaseTime(request)
	if err != nil {
		return err
	}
	log.Printf("success(%d)! requestId = %s\t autoReleaseTime = %s",
		tea.Int32Value(response.StatusCode),
		tea.StringValue(response.Body.RequestId),
		releaseTime,
	)
	return nil
}

// extractAliyunPublicIP extracts the public IP or EIP from an Aliyun ECS instance regardless of status.
func extractAliyunPublicIP(inst *ecs.DescribeInstancesResponseBodyInstancesInstance) string {
	if inst == nil {
		return ""
	}
	if inst.PublicIpAddress != nil && len(inst.PublicIpAddress.IpAddress) > 0 && tea.StringValue(inst.PublicIpAddress.IpAddress[0]) != "" {
		return tea.StringValue(inst.PublicIpAddress.IpAddress[0])
	}
	if inst.EipAddress != nil && tea.StringValue(inst.EipAddress.IpAddress) != "" {
		return tea.StringValue(inst.EipAddress.IpAddress)
	}
	return ""
}

// extractAliyunPrivateIP extracts the primary private IP from an Aliyun ECS instance.
func extractAliyunPrivateIP(inst *ecs.DescribeInstancesResponseBodyInstancesInstance) string {
	if inst == nil {
		return ""
	}
	if inst.VpcAttributes != nil && inst.VpcAttributes.PrivateIpAddress != nil && len(inst.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
		return tea.StringValue(inst.VpcAttributes.PrivateIpAddress.IpAddress[0])
	}
	if inst.InnerIpAddress != nil && len(inst.InnerIpAddress.IpAddress) > 0 {
		return tea.StringValue(inst.InnerIpAddress.IpAddress[0])
	}
	return ""
}

// Describeinstances queries Alibaba Cloud ECS instance status and public IP.
func Describeinstances(ecsClient *ecs.Client, region, instanceid string) (status, ipaddress string, err error) {
	status, ipaddress, _, err = DescribeinstanceDetail(ecsClient, region, instanceid)
	return status, ipaddress, err
}

// DescribeinstanceDetail queries Alibaba Cloud ECS instance status, public IP (in any state), and creation time.
func DescribeinstanceDetail(ecsClient *ecs.Client, region, instanceid string) (status, ipaddress, creationTime string, err error) {
	request := &ecs.DescribeInstancesRequest{
		RegionId:    tea.String(region),
		InstanceIds: tea.String(instanceid),
	}
	response, err := ecsClient.DescribeInstances(request)
	if err != nil {
		return "", "", "", err
	}
	if response.Body == nil || response.Body.Instances == nil || len(response.Body.Instances.Instance) == 0 {
		return "Pending", "", "", nil
	}
	inst := response.Body.Instances.Instance[0]
	status = tea.StringValue(inst.Status)
	ipaddress = extractAliyunPublicIP(inst)
	creationTime = tea.StringValue(inst.CreationTime)
	statusCode := tea.Int32Value(response.StatusCode)
	log.Printf("success(%d)! status = %s\t ip = %s", statusCode, status, ipaddress)
	return status, ipaddress, creationTime, nil
}

// FetchAliyunInstances queries instances in the region (optionally filtered by IDs).
func FetchAliyunInstances(ecsClient *ecs.Client, region string, ids []string) ([]InstanceSummary, error) {
	var summaries []InstanceSummary
	pageNumber := int32(1)
	pageSize := int32(50)

	var idsJSON string
	if len(ids) > 0 {
		b, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		idsJSON = string(b)
	}

	for {
		req := &ecs.DescribeInstancesRequest{
			RegionId:   tea.String(region),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		}
		if idsJSON != "" {
			req.InstanceIds = tea.String(idsJSON)
		}
		resp, err := ecsClient.DescribeInstances(req)
		if err != nil {
			return nil, err
		}
		if resp.Body == nil || resp.Body.Instances == nil || len(resp.Body.Instances.Instance) == 0 {
			break
		}
		for _, inst := range resp.Body.Instances.Instance {
			if inst == nil {
				continue
			}
			name := tea.StringValue(inst.InstanceName)
			if name == "" {
				name = tea.StringValue(inst.HostName)
			}
			summaries = append(summaries, InstanceSummary{
				ID:              tea.StringValue(inst.InstanceId),
				Name:            name,
				Status:          tea.StringValue(inst.Status),
				PublicIP:        extractAliyunPublicIP(inst),
				PrivateIP:       extractAliyunPrivateIP(inst),
				InstanceType:    tea.StringValue(inst.InstanceType),
				CreatedTime:     tea.StringValue(inst.CreationTime),
				AutoReleaseTime: tea.StringValue(inst.AutoReleaseTime),
			})
		}
		totalCount := tea.Int32Value(resp.Body.TotalCount)
		if int32(len(summaries)) >= totalCount || len(resp.Body.Instances.Instance) < int(pageSize) {
			break
		}
		pageNumber++
	}
	return summaries, nil
}

// DeleteAliyunInstances forcefully deletes one or more Alibaba Cloud ECS instances.
func DeleteAliyunInstances(ecsClient *ecs.Client, ids []string) error {
	for _, id := range ids {
		req := &ecs.DeleteInstanceRequest{
			InstanceId: tea.String(id),
			Force:      tea.Bool(true),
		}
		resp, err := ecsClient.DeleteInstance(req)
		if err != nil {
			return fmt.Errorf("delete instance %s failed: %w", id, err)
		}
		log.Printf("deleted instance %s (requestId = %s)", id, tea.StringValue(resp.Body.RequestId))
	}
	return nil
}

// ExtendAliyunInstances extends the auto-release time of one or more Alibaba Cloud ECS instances.
func ExtendAliyunInstances(ecsClient *ecs.Client, region string, ids []string, extendHours int64) error {
	instances, err := FetchAliyunInstances(ecsClient, region, ids)
	if err != nil {
		return err
	}
	instMap := make(map[string]InstanceSummary, len(instances))
	for _, inst := range instances {
		instMap[inst.ID] = inst
	}

	now := time.Now().UTC()
	for _, id := range ids {
		inst, ok := instMap[id]
		if !ok {
			return fmt.Errorf("instance %s not found in region %s", id, region)
		}
		newReleaseAt := calcExtendReleaseTime(inst.AutoReleaseTime, inst.CreatedTime, now, extendHours)
		if err := AutoreleaseAt(ecsClient, region, id, newReleaseAt); err != nil {
			return fmt.Errorf("extend instance %s failed: %w", id, err)
		}
		log.Printf("extended instance %s: previous autoReleaseTime = %q -> new autoReleaseTime = %q",
			id, inst.AutoReleaseTime, newReleaseAt.Format("2006-01-02T15:04:05Z"))
	}
	return nil
}

func dispatchTencent(action, region, hostname string, releasetime int64, maxprice float64, ids []string) {
	secretID := os.Getenv("TENCENTCLOUD_SECRET_ID")
	secretKey := os.Getenv("TENCENTCLOUD_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		log.Fatal("environment variables TENCENTCLOUD_SECRET_ID and TENCENTCLOUD_SECRET_KEY are required")
	}

	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cvmClient, err := cvm.NewClient(credential, region, cpf)
	if err != nil {
		log.Panicf("%v", err)
	}

	switch action {
	case "create":
		password := Randpassword(12)
		runTencent(cvmClient, hostname, password, releasetime, maxprice)
	case "list":
		instances, err := FetchTencentInstances(cvmClient, ids)
		if err != nil {
			log.Panicf("%v", err)
		}
		printInstanceTable(instances)
	case "delete":
		if err := DeleteTencentInstances(cvmClient, ids); err != nil {
			log.Panicf("%v", err)
		}
	case "extend":
		if err := ExtendTencentInstances(cvmClient, ids, releasetime); err != nil {
			log.Panicf("%v", err)
		}
	}
}

func runTencent(cvmClient *cvm.Client, hostname, password string, releasetime int64, maxprice float64) {
	zone := os.Getenv("TENCENTCLOUD_ZONE")
	if zone == "" {
		log.Fatal("environment variable TENCENTCLOUD_ZONE is required for creating a Tencent Cloud instance")
	}

	instanceType := os.Getenv("TENCENTCLOUD_INSTANCE_TYPE")
	if instanceType == "" {
		instanceType = "SA2.MEDIUM2"
	}

	var err error
	imageID := os.Getenv("TENCENTCLOUD_IMAGE_ID")
	if imageID == "" {
		imageID, err = ResolveTencentPublicImage(cvmClient, instanceType)
		if err != nil {
			log.Panicf("%v", err)
		}
	}

	createStartTime := time.Now().UTC()
	instanceID, err := CreateTencentSpotInstance(cvmClient, zone, instanceType, imageID, hostname, password, releasetime, maxprice, createStartTime)
	if err != nil {
		log.Panicf("%v", err)
	}

	allocatedIP := false
	for {
		status, ipaddress, err := DescribeTencentInstance(cvmClient, instanceID)
		if err != nil {
			log.Panicf("%v", err)
		}
		if status == "RUNNING" {
			if ipaddress != "" {
				log.Printf("using existing ipaddress :\t %s ", ipaddress)
				log.Printf("ip :\t %s \t password :\t %s ", ipaddress, password)
				break
			}
			if !allocatedIP {
				log.Printf("no public IP detected on running instance %s, requesting public IP allocation...", instanceID)
				if err := AllocateTencentPublicIP(cvmClient, instanceID); err != nil {
					log.Panicf("%v", err)
				}
				allocatedIP = true
			}
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
func CreateTencentSpotInstance(cvmClient *cvm.Client, zone, instanceType, imageID, hostname, password string, releasetime int64, maxprice float64, createStartTime time.Time) (string, error) {
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

	maxPriceStr := strconv.FormatFloat(maxprice, 'f', -1, 64)
	request.InstanceMarketOptions = &cvm.InstanceMarketOptionsRequest{
		MarketType: common.StringPtr("spot"),
		SpotOptions: &cvm.SpotMarketOptions{
			MaxPrice:         common.StringPtr(maxPriceStr),
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
		actionAt := calcCreateReleaseTime("", createStartTime, releasetime)
		actionTime := actionAt.UTC().Format("2006-01-02T15:04:05Z")
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

// AllocateTencentPublicIP requests public IP allocation for an instance without one by resetting outbound bandwidth.
func AllocateTencentPublicIP(cvmClient *cvm.Client, instanceID string) error {
	req := cvm.NewResetInstancesInternetMaxBandwidthRequest()
	req.InstanceIds = []*string{common.StringPtr(instanceID)}
	req.InternetAccessible = &cvm.InternetAccessible{
		InternetChargeType:      common.StringPtr("TRAFFIC_POSTPAID_BY_HOUR"),
		InternetMaxBandwidthOut: common.Int64Ptr(100),
		PublicIpAssigned:        common.BoolPtr(true),
	}
	resp, err := cvmClient.ResetInstancesInternetMaxBandwidth(req)
	if err != nil {
		return err
	}
	requestID := ""
	if resp.Response != nil && resp.Response.RequestId != nil {
		requestID = *resp.Response.RequestId
	}
	log.Printf("requested public IP allocation for %s (requestId = %s)", instanceID, requestID)
	return nil
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

// FetchTencentInstances queries Tencent Cloud CVM instances and their active scheduled termination timers.
func FetchTencentInstances(cvmClient *cvm.Client, ids []string) ([]InstanceSummary, error) {
	var rawInstances []*cvm.Instance
	offset := int64(0)
	limit := int64(100)

	for {
		req := cvm.NewDescribeInstancesRequest()
		if len(ids) > 0 {
			req.InstanceIds = common.StringPtrs(ids)
		} else {
			req.Offset = common.Int64Ptr(offset)
			req.Limit = common.Int64Ptr(limit)
		}
		resp, err := cvmClient.DescribeInstances(req)
		if err != nil {
			return nil, err
		}
		if resp.Response == nil || len(resp.Response.InstanceSet) == 0 {
			break
		}
		rawInstances = append(rawInstances, resp.Response.InstanceSet...)
		totalCount := int64(0)
		if resp.Response.TotalCount != nil {
			totalCount = *resp.Response.TotalCount
		}
		if len(ids) > 0 || int64(len(rawInstances)) >= totalCount || int64(len(resp.Response.InstanceSet)) < limit {
			break
		}
		offset += limit
	}

	if len(rawInstances) == 0 {
		return nil, nil
	}

	var queryIDs []string
	for _, inst := range rawInstances {
		if inst != nil && inst.InstanceId != nil {
			queryIDs = append(queryIDs, *inst.InstanceId)
		}
	}

	timerTimeMap := make(map[string]string)
	timerIDsMap := make(map[string][]string)
	if len(queryIDs) > 0 {
		timerReq := cvm.NewDescribeInstancesActionTimerRequest()
		timerReq.InstanceIds = common.StringPtrs(queryIDs)
		timerReq.TimerAction = common.StringPtr("TerminateInstances")
		timerReq.StatusList = []*string{common.StringPtr("UNDO")}
		timerResp, err := cvmClient.DescribeInstancesActionTimer(timerReq)
		if err == nil && timerResp.Response != nil {
			for _, t := range timerResp.Response.ActionTimers {
				if t == nil || t.InstanceId == nil {
					continue
				}
				iid := *t.InstanceId
				if t.ActionTime != nil && *t.ActionTime != "" {
					timerTimeMap[iid] = *t.ActionTime
				}
				if t.ActionTimerId != nil && *t.ActionTimerId != "" {
					timerIDsMap[iid] = append(timerIDsMap[iid], *t.ActionTimerId)
				}
			}
		}
	}

	summaries := make([]InstanceSummary, 0, len(rawInstances))
	for _, inst := range rawInstances {
		if inst == nil || inst.InstanceId == nil {
			continue
		}
		iid := *inst.InstanceId
		var name, status, pubIP, privIP, instType, createdTime string
		if inst.InstanceName != nil {
			name = *inst.InstanceName
		}
		if inst.InstanceState != nil {
			status = *inst.InstanceState
		}
		if len(inst.PublicIpAddresses) > 0 && inst.PublicIpAddresses[0] != nil {
			pubIP = *inst.PublicIpAddresses[0]
		}
		if len(inst.PrivateIpAddresses) > 0 && inst.PrivateIpAddresses[0] != nil {
			privIP = *inst.PrivateIpAddresses[0]
		}
		if inst.InstanceType != nil {
			instType = *inst.InstanceType
		}
		if inst.CreatedTime != nil {
			createdTime = *inst.CreatedTime
		}
		summaries = append(summaries, InstanceSummary{
			ID:              iid,
			Name:            name,
			Status:          status,
			PublicIP:        pubIP,
			PrivateIP:       privIP,
			InstanceType:    instType,
			CreatedTime:     createdTime,
			AutoReleaseTime: timerTimeMap[iid],
			ActionTimerIDs:  timerIDsMap[iid],
		})
	}
	return summaries, nil
}

// DeleteTencentInstances terminates one or more Tencent Cloud CVM instances.
func DeleteTencentInstances(cvmClient *cvm.Client, ids []string) error {
	req := cvm.NewTerminateInstancesRequest()
	req.InstanceIds = common.StringPtrs(ids)
	resp, err := cvmClient.TerminateInstances(req)
	if err != nil {
		return err
	}
	requestID := ""
	if resp.Response != nil && resp.Response.RequestId != nil {
		requestID = *resp.Response.RequestId
	}
	log.Printf("terminated instances %s (requestId = %s)", strings.Join(ids, ","), requestID)
	return nil
}

// ExtendTencentInstances extends the scheduled termination time of one or more Tencent Cloud CVM instances.
func ExtendTencentInstances(cvmClient *cvm.Client, ids []string, extendHours int64) error {
	instances, err := FetchTencentInstances(cvmClient, ids)
	if err != nil {
		return err
	}
	instMap := make(map[string]InstanceSummary, len(instances))
	for _, inst := range instances {
		instMap[inst.ID] = inst
	}

	now := time.Now().UTC()
	for _, id := range ids {
		inst, ok := instMap[id]
		if !ok {
			return fmt.Errorf("instance %s not found", id)
		}
		newReleaseAt := calcExtendReleaseTime(inst.AutoReleaseTime, inst.CreatedTime, now, extendHours)
		newActionTime := newReleaseAt.UTC().Format("2006-01-02T15:04:05Z")

		if len(inst.ActionTimerIDs) > 0 {
			delReq := cvm.NewDeleteInstancesActionTimerRequest()
			delReq.ActionTimerIds = common.StringPtrs(inst.ActionTimerIDs)
			if _, err := cvmClient.DeleteInstancesActionTimer(delReq); err != nil {
				return fmt.Errorf("delete old action timer for instance %s failed: %w", id, err)
			}
		}

		importReq := cvm.NewImportInstancesActionTimerRequest()
		importReq.InstanceIds = []*string{common.StringPtr(id)}
		importReq.ActionTimer = &cvm.ActionTimer{
			TimerAction: common.StringPtr("TerminateInstances"),
			ActionTime:  common.StringPtr(newActionTime),
			Externals: &cvm.Externals{
				ReleaseAddress: common.BoolPtr(true),
			},
		}
		resp, err := cvmClient.ImportInstancesActionTimer(importReq)
		if err != nil {
			return fmt.Errorf("import new action timer for instance %s failed: %w", id, err)
		}
		requestID := ""
		if resp.Response != nil && resp.Response.RequestId != nil {
			requestID = *resp.Response.RequestId
		}
		log.Printf("extended instance %s (requestId = %s): previous actionTime = %q -> new actionTime = %q",
			id, requestID, inst.AutoReleaseTime, newActionTime)
	}
	return nil
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

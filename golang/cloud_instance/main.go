package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"regexp"
	"sort"
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
	upperRegexp       = regexp.MustCompile(`[A-Z]`)
	lowerRegexp       = regexp.MustCompile(`[a-z]`)
	numberRegexp      = regexp.MustCompile(`[0-9]`)
	otherRegexp       = regexp.MustCompile(`[^0-9a-zA-Z]`)
	almaVersionRegexp = regexp.MustCompile(`(?i)almalinux(?:\s+server|\s+linux)?[\s_-]*(\d+)(?:[._](\d+))?(?:[._](\d+))?`)
	alibaseDateRegexp = regexp.MustCompile(`_(\d{8})\.vhd`)
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

// ZoneSummary holds normalized availability zone metadata.
type ZoneSummary struct {
	Zone      string
	ZoneID    string
	Name      string
	State     string
	IsDefault bool
}

// PublicImageSummary holds normalized public OS image metadata across cloud providers.
type PublicImageSummary struct {
	ImageID     string
	ImageName   string
	OSName      string
	Platform    string
	Arch        string
	SizeGiB     int64
	CreatedTime string
	ImageFamily string
	IsDefault   bool
}

// CreateSpecOptions holds instance specification selection preferences for creation.
type CreateSpecOptions struct {
	CPU          int
	Memory       float64
	InstanceType string
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
		cpuFlag     int
		memoryFlag  float64
		typeFlag    string
	)
	flag.StringVar(&provider, "provider", "aliyun", "cloud provider: aliyun or tencent")
	flag.StringVar(&action, "action", "create", "action to perform: create, list, delete, extend, zones, or images")
	flag.StringVar(&region, "region", "", "cloud region ID (default: cn-shanghai for aliyun, ap-shanghai for tencent)")
	flag.StringVar(&idFlag, "id", "", "instance ID(s), comma-separated (required for delete/extend, optional for list)")
	flag.StringVar(&hostname, "hostname", "example", "instance hostname (for create)")
	flag.Int64Var(&releasetime, "releasetime", 1, "auto release duration in hours (for create/extend)")
	flag.Float64Var(&maxprice, "maxprice", 0.98, "max spot price per hour (for create)")
	flag.IntVar(&cpuFlag, "cpu", 1, "target vCPU core count for auto-selecting X86 instance type (for create)")
	flag.Float64Var(&memoryFlag, "memory", 1, "target memory size in GiB for auto-selecting X86 instance type (for create)")
	flag.StringVar(&typeFlag, "type", "", "preferred instance type (optional, overrides auto-selection priority when in stock)")
	flag.Parse()

	provider = strings.ToLower(strings.TrimSpace(provider))
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "zone":
		action = "zones"
	case "image":
		action = "images"
	}
	ids := parseInstanceIDs(idFlag)

	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})
	specOpts := resolveCreateSpecOptions(provider, cpuFlag, memoryFlag, typeFlag, explicitFlags)

	switch action {
	case "create":
		if maxprice <= 0 {
			log.Fatalf("invalid -maxprice %.4f: must be greater than 0", maxprice)
		}
		if releasetime <= 0 {
			log.Fatalf("invalid -releasetime %d: must be greater than 0", releasetime)
		}
		if specOpts.CPU <= 0 {
			log.Fatalf("invalid -cpu %d: must be greater than 0", specOpts.CPU)
		}
		if specOpts.Memory <= 0 {
			log.Fatalf("invalid -memory %.2f: must be greater than 0", specOpts.Memory)
		}
	case "list", "zones", "images":
		// query actions
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
		log.Fatalf("unsupported action %q: must be 'create', 'list', 'delete', 'extend', 'zones', or 'images'", action)
	}

	switch provider {
	case "aliyun":
		if region == "" {
			region = "cn-shanghai"
		}
		dispatchAliyun(action, region, hostname, releasetime, maxprice, ids, specOpts)
	case "tencent":
		if region == "" {
			region = "ap-shanghai"
		}
		dispatchTencent(action, region, hostname, releasetime, maxprice, ids, specOpts)
	default:
		log.Fatalf("unsupported provider %q: must be 'aliyun' or 'tencent'", provider)
	}
}

func resolveCreateSpecOptions(provider string, cpuFlag int, memoryFlag float64, typeFlag string, explicitFlags map[string]bool) CreateSpecOptions {
	opts := CreateSpecOptions{
		CPU:          cpuFlag,
		Memory:       memoryFlag,
		InstanceType: strings.TrimSpace(typeFlag),
	}

	if !explicitFlags["cpu"] {
		for _, envKey := range providerEnvKeys(provider, "CPU") {
			if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
				if v, err := strconv.Atoi(raw); err == nil && v > 0 {
					opts.CPU = v
					break
				}
			}
		}
	}
	if !explicitFlags["memory"] {
		for _, envKey := range providerEnvKeys(provider, "MEMORY") {
			if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
				if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
					opts.Memory = v
					break
				}
			}
		}
	}
	if opts.InstanceType == "" {
		for _, envKey := range providerEnvKeys(provider, "INSTANCE_TYPE") {
			if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
				opts.InstanceType = raw
				break
			}
		}
	}
	return opts
}

func providerEnvKeys(provider, suffix string) []string {
	switch provider {
	case "aliyun":
		return []string{"ALIBABA_CLOUD_" + suffix, "CLOUD_INSTANCE_" + suffix}
	case "tencent":
		return []string{"TENCENTCLOUD_" + suffix, "CLOUD_INSTANCE_" + suffix}
	default:
		return []string{"CLOUD_INSTANCE_" + suffix}
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

func printZoneTable(zones []ZoneSummary) {
	if len(zones) == 0 {
		fmt.Println("No zones found.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ZONE\tZONE_ID\tNAME\tSTATE\tDEFAULT")
	for _, z := range zones {
		zoneID := z.ZoneID
		if zoneID == "" {
			zoneID = "-"
		}
		name := z.Name
		if name == "" {
			name = "-"
		}
		state := z.State
		if state == "" {
			state = "-"
		}
		defMark := "-"
		if z.IsDefault {
			defMark = "* (default)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", z.Zone, zoneID, name, state, defMark)
	}
	_ = w.Flush()
}

func printImageTable(images []PublicImageSummary) {
	if len(images) == 0 {
		fmt.Println("No public images found.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "IMAGE_ID\tOS_NAME\tPLATFORM\tARCH\tSIZE_GIB\tCREATED_TIME\tDEFAULT")
	for _, img := range images {
		osName := img.OSName
		if osName == "" {
			osName = img.ImageName
		}
		if osName == "" {
			osName = "-"
		}
		platform := img.Platform
		if platform == "" {
			platform = "-"
		}
		arch := img.Arch
		if arch == "" {
			arch = "-"
		}
		created := img.CreatedTime
		if created == "" {
			created = "-"
		}
		defMark := "-"
		if img.IsDefault {
			defMark = "* (default)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			img.ImageID, osName, platform, arch, img.SizeGiB, created, defMark)
	}
	_ = w.Flush()
}

func isAlmaLinuxImage(texts ...string) bool {
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), "almalinux") {
			return true
		}
	}
	return false
}

// extractAlmaLinuxVersion parses the numeric [major, minor, patch] version of AlmaLinux (e.g. 10.1 -> [10, 1, 0])
// across image metadata fields so 10.x is ranked higher than 9.x and 8.x.
func extractAlmaLinuxVersion(texts ...string) [3]int {
	var best [3]int
	for _, text := range texts {
		matches := almaVersionRegexp.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			var cur [3]int
			if len(m) > 1 && m[1] != "" {
				cur[0], _ = strconv.Atoi(m[1])
			}
			if len(m) > 2 && m[2] != "" {
				cur[1], _ = strconv.Atoi(m[2])
			}
			if len(m) > 3 && m[3] != "" {
				cur[2], _ = strconv.Atoi(m[3])
			}
			if compareVersionTuple(cur, best) > 0 {
				best = cur
			}
		}
	}
	return best
}

func compareVersionTuple(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func extractImageCreatedTime(img PublicImageSummary) (time.Time, bool) {
	if t, ok := parseCloudTime(img.CreatedTime); ok {
		return t, true
	}
	if m := alibaseDateRegexp.FindStringSubmatch(img.ImageID); len(m) > 1 {
		if t, err := time.Parse("20060102", m[1]); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func comparePublicImageDesc(a, b PublicImageSummary) bool {
	aAlma := isAlmaLinuxImage(a.Platform, a.OSName, a.ImageName, a.ImageFamily, a.ImageID)
	bAlma := isAlmaLinuxImage(b.Platform, b.OSName, b.ImageName, b.ImageFamily, b.ImageID)
	if aAlma != bAlma {
		return aAlma
	}

	verA := extractAlmaLinuxVersion(a.OSName, a.ImageName, a.ImageFamily, a.ImageID)
	verB := extractAlmaLinuxVersion(b.OSName, b.ImageName, b.ImageFamily, b.ImageID)
	if cmp := compareVersionTuple(verA, verB); cmp != 0 {
		return cmp > 0
	}

	timeA, okA := extractImageCreatedTime(a)
	timeB, okB := extractImageCreatedTime(b)
	if okA && okB && !timeA.Equal(timeB) {
		return timeA.After(timeB)
	}
	if a.CreatedTime != b.CreatedTime && a.CreatedTime != "" && b.CreatedTime != "" {
		return a.CreatedTime > b.CreatedTime
	}
	return a.ImageID > b.ImageID
}

func sortPublicImagesDesc(images []PublicImageSummary) {
	sort.SliceStable(images, func(i, j int) bool {
		return comparePublicImageDesc(images[i], images[j])
	})
	for i := range images {
		images[i].IsDefault = (i == 0)
	}
}

// AliyunVSwitchInfo holds metadata for an Aliyun VSwitch and its availability zone.
type AliyunVSwitchInfo struct {
	VSwitchID           string
	ZoneID              string
	VpcID               string
	AvailableIPCount    int64
	IsPreferredVSwitch  bool
	IsPreferredZone     bool
}

// AliyunInstanceTypeSpec holds normalized hardware metadata from DescribeInstanceTypes.
type AliyunInstanceTypeSpec struct {
	InstanceType string
	CPU          int
	Memory       float64
	Family       string
	FamilyLevel  string
}

// AliyunCandidate represents a ranked Aliyun spot instance creation candidate.
type AliyunCandidate struct {
	VSwitchID          string
	ZoneID             string
	InstanceType       string
	SystemDiskCategory string
	CPU                int
	Memory             float64
	Family             string
	SpotPrice          float64
	StockCategory      string
	ExplicitMatch      bool
	ExactMatch         bool
	PreferredVSwitch   bool
	PreferredZone      bool
}

func dispatchAliyun(action, region, hostname string, releasetime int64, maxprice float64, ids []string, specOpts CreateSpecOptions) {
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
		runAliyun(ecsClient, region, hostname, password, releasetime, maxprice, specOpts)
	case "list":
		instances, err := FetchAliyunInstances(ecsClient, region, ids)
		if err != nil {
			log.Panicf("%v", err)
		}
		printInstanceTable(instances)
	case "zones":
		zones, err := FetchAliyunZones(ecsClient, region)
		if err != nil {
			log.Panicf("%v", err)
		}
		printZoneTable(zones)
	case "images":
		images, err := FetchAliyunPublicImages(ecsClient, region, specOpts.InstanceType)
		if err != nil {
			log.Panicf("%v", err)
		}
		printImageTable(images)
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

func runAliyun(ecsClient *ecs.Client, region, hostname, password string, releasetime int64, maxprice float64, specOpts CreateSpecOptions) {
	securityGroupID := strings.TrimSpace(os.Getenv("ALIBABA_CLOUD_SECURITY_GROUP_ID"))
	preferredVSwitchID := strings.TrimSpace(os.Getenv("ALIBABA_CLOUD_VSWITCH_ID"))
	if securityGroupID == "" {
		log.Fatal("environment variable ALIBABA_CLOUD_SECURITY_GROUP_ID is required for creating an Aliyun instance")
	}

	createStartTime := time.Now().UTC()
	instanceid, err := CreateinstanceWithOptions(ecsClient, region, hostname, password, maxprice, securityGroupID, preferredVSwitchID, specOpts)
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

// Createinstance creates a spot ECS instance on Alibaba Cloud using default 1C1G X86 auto-selection.
func Createinstance(ecsClient *ecs.Client, region, hostname, password string, maxprice float64) (instanceid string, err error) {
	specOpts := resolveCreateSpecOptions("aliyun", 1, 1, "", nil)
	return CreateinstanceWithOptions(
		ecsClient,
		region,
		hostname,
		password,
		maxprice,
		strings.TrimSpace(os.Getenv("ALIBABA_CLOUD_SECURITY_GROUP_ID")),
		strings.TrimSpace(os.Getenv("ALIBABA_CLOUD_VSWITCH_ID")),
		specOpts,
	)
}

// CreateinstanceWithOptions selects an in-stock X86 instance type and VSwitch (with cross-zone fallback) and creates a spot ECS instance.
func CreateinstanceWithOptions(ecsClient *ecs.Client, region, hostname, password string, maxprice float64, securityGroupID, preferredVSwitchID string, specOpts CreateSpecOptions) (string, error) {
	vswitches, err := ResolveAliyunVSwitches(ecsClient, region, securityGroupID, preferredVSwitchID)
	if err != nil {
		return "", err
	}

	candidates, err := SelectAliyunCandidates(ecsClient, region, vswitches, specOpts, maxprice)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no available Aliyun spot instance types in stock for region=%s (target=%dC%.1fG, type=%q, maxprice=%.4f)",
			region, specOpts.CPU, specOpts.Memory, specOpts.InstanceType, maxprice)
	}

	imageID := ResolveAliyunPublicImage(ecsClient, region, "")

	var lastErr error
	for idx, cand := range candidates {
		diskCategories := ResolveAliyunSystemDiskCategories(ecsClient, region, cand.ZoneID, cand.InstanceType)
		for _, diskCategory := range diskCategories {
			candWithDisk := cand
			candWithDisk.SystemDiskCategory = diskCategory
			log.Printf("[aliyun][candidate %d/%d] trying zone=%s vswitch=%s type=%s (%dC%.1fG) disk=%s spotPrice=%.4f",
				idx+1, len(candidates), candWithDisk.ZoneID, candWithDisk.VSwitchID,
				candWithDisk.InstanceType, candWithDisk.CPU, candWithDisk.Memory,
				candWithDisk.SystemDiskCategory, candWithDisk.SpotPrice)

			instanceID, createErr := createSingleAliyunInstance(ecsClient, region, securityGroupID, hostname, password, maxprice, imageID, candWithDisk)
			if createErr == nil {
				return instanceID, nil
			}
			lastErr = createErr
			if isAliyunDiskCategoryError(createErr) {
				log.Printf("[aliyun] system disk category %s unsupported/no-stock for %s in %s, trying next disk category: %v",
					diskCategory, cand.InstanceType, cand.ZoneID, createErr)
				continue
			}
			if isAliyunRetryableCreateError(createErr) {
				log.Printf("[aliyun] candidate %s in %s (vswitch=%s) unavailable, falling back to next candidate: %v",
					cand.InstanceType, cand.ZoneID, cand.VSwitchID, createErr)
				break
			}
			return "", createErr
		}
	}
	return "", fmt.Errorf("all %d Aliyun spot candidates failed in region %s; last error: %w", len(candidates), region, lastErr)
}

func createSingleAliyunInstance(ecsClient *ecs.Client, region, securityGroupID, hostname, password string, maxprice float64, imageID string, cand AliyunCandidate) (string, error) {
	request := &ecs.CreateInstanceRequest{
		RegionId:                    tea.String(region),
		ZoneId:                      tea.String(cand.ZoneID),
		InstanceType:                tea.String(cand.InstanceType),
		SecurityGroupId:             tea.String(securityGroupID),
		InternetChargeType:          tea.String("PayByTraffic"),
		InternetMaxBandwidthIn:      tea.Int32(100),
		InternetMaxBandwidthOut:     tea.Int32(100),
		IoOptimized:                 tea.String("optimized"),
		VSwitchId:                   tea.String(cand.VSwitchID),
		InstanceChargeType:          tea.String("PostPaid"),
		SpotStrategy:                tea.String("SpotWithPriceLimit"),
		SpotPriceLimit:              tea.Float32(float32(maxprice)),
		HostName:                    tea.String(hostname),
		Password:                    tea.String(password),
		InstanceName:                tea.String(hostname),
		SecurityEnhancementStrategy: tea.String("Deactive"),
	}
	if imageID != "" {
		request.ImageId = tea.String(imageID)
	} else {
		request.ImageFamily = tea.String("acs:almalinux_9_2_x64")
	}
	if cand.SystemDiskCategory != "" {
		request.SystemDisk = &ecs.CreateInstanceRequestSystemDisk{
			Category: tea.String(cand.SystemDiskCategory),
		}
	}
	response, err := ecsClient.CreateInstance(request)
	if err != nil {
		return "", err
	}
	instanceid := tea.StringValue(response.Body.InstanceId)
	log.Printf("success(%d)! requestId = %s\t instanceId = %s\t zone = %s\t type = %s\t disk = %s",
		tea.Int32Value(response.StatusCode),
		tea.StringValue(response.Body.RequestId),
		instanceid,
		cand.ZoneID,
		cand.InstanceType,
		cand.SystemDiskCategory,
	)
	return instanceid, nil
}

// FetchAliyunZones queries availability zones in the Aliyun region.
func FetchAliyunZones(ecsClient *ecs.Client, region string) ([]ZoneSummary, error) {
	req := &ecs.DescribeZonesRequest{
		RegionId: tea.String(region),
	}
	resp, err := ecsClient.DescribeZones(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeZones failed: %w", err)
	}
	var zones []ZoneSummary
	if resp.Body == nil || resp.Body.Zones == nil {
		return zones, nil
	}
	for _, z := range resp.Body.Zones.Zone {
		if z == nil {
			continue
		}
		zoneID := tea.StringValue(z.ZoneId)
		if zoneID == "" {
			continue
		}
		state := "AVAILABLE"
		if z.AvailableResourceCreation != nil && len(z.AvailableResourceCreation.ResourceTypes) == 0 {
			state = "LIMITED"
		}
		zones = append(zones, ZoneSummary{
			Zone:   zoneID,
			ZoneID: zoneID,
			Name:   tea.StringValue(z.LocalName),
			State:  state,
		})
	}
	sort.SliceStable(zones, func(i, j int) bool {
		return zones[i].Zone > zones[j].Zone
	})
	if len(zones) > 0 {
		zones[0].IsDefault = true
	}
	return zones, nil
}

// FetchAliyunPublicImages queries official Aliyun public x86_64 AlmaLinux images and ranks the latest version first.
func FetchAliyunPublicImages(ecsClient *ecs.Client, region, instanceType string) ([]PublicImageSummary, error) {
	var allAlma []PublicImageSummary
	pageNumber := int32(1)
	pageSize := int32(100)

	for {
		req := &ecs.DescribeImagesRequest{
			RegionId:        tea.String(region),
			ImageOwnerAlias: tea.String("system"),
			OSType:          tea.String("linux"),
			Architecture:    tea.String("x86_64"),
			Status:          tea.String("Available"),
			ActionType:      tea.String("CreateEcs"),
			PageNumber:      tea.Int32(pageNumber),
			PageSize:        tea.Int32(pageSize),
		}
		if instanceType != "" {
			req.InstanceType = tea.String(instanceType)
		}
		resp, err := ecsClient.DescribeImages(req)
		if err != nil {
			return nil, fmt.Errorf("DescribeImages failed: %w", err)
		}
		if resp.Body == nil || resp.Body.Images == nil || len(resp.Body.Images.Image) == 0 {
			break
		}
		for _, img := range resp.Body.Images.Image {
			if img == nil {
				continue
			}
			imageID := tea.StringValue(img.ImageId)
			imageName := tea.StringValue(img.ImageName)
			osName := tea.StringValue(img.OSNameEn)
			if osName == "" {
				osName = tea.StringValue(img.OSName)
			}
			platform := tea.StringValue(img.Platform)
			family := tea.StringValue(img.ImageFamily)
			if !isAlmaLinuxImage(platform, osName, imageName, family, imageID) {
				continue
			}
			allAlma = append(allAlma, PublicImageSummary{
				ImageID:     imageID,
				ImageName:   imageName,
				OSName:      osName,
				Platform:    platform,
				Arch:        tea.StringValue(img.Architecture),
				SizeGiB:     int64(tea.Int32Value(img.Size)),
				CreatedTime: tea.StringValue(img.CreationTime),
				ImageFamily: family,
			})
		}
		total := tea.Int32Value(resp.Body.TotalCount)
		if pageNumber*pageSize >= total || len(resp.Body.Images.Image) < int(pageSize) {
			break
		}
		pageNumber++
	}

	sortPublicImagesDesc(allAlma)
	return allAlma, nil
}

// ResolveAliyunPublicImage resolves the latest x86_64 AlmaLinux public image ID in the Aliyun region.
func ResolveAliyunPublicImage(ecsClient *ecs.Client, region, instanceType string) string {
	if fixed := strings.TrimSpace(os.Getenv("ALIBABA_CLOUD_IMAGE_ID")); fixed != "" {
		return fixed
	}
	images, err := FetchAliyunPublicImages(ecsClient, region, instanceType)
	if err != nil || len(images) == 0 {
		return ""
	}
	log.Printf("resolved latest Aliyun public image: %s (%s)", images[0].ImageID, images[0].OSName)
	return images[0].ImageID
}

// ResolveAliyunVSwitches discovers available VSwitches in the same VPC as the preferred VSwitch or Security Group.
func ResolveAliyunVSwitches(ecsClient *ecs.Client, region, securityGroupID, preferredVSwitchID string) ([]AliyunVSwitchInfo, error) {
	var (
		vpcID         string
		preferredZone string
		vswitches     []AliyunVSwitchInfo
		seen          = make(map[string]bool)
	)

	if preferredVSwitchID != "" {
		req := &ecs.DescribeVSwitchesRequest{
			RegionId:  tea.String(region),
			VSwitchId: tea.String(preferredVSwitchID),
		}
		resp, err := ecsClient.DescribeVSwitches(req)
		if err != nil {
			return nil, fmt.Errorf("describe preferred vswitch %s failed: %w", preferredVSwitchID, err)
		}
		if resp.Body != nil && resp.Body.VSwitches != nil && len(resp.Body.VSwitches.VSwitch) > 0 {
			vsw := resp.Body.VSwitches.VSwitch[0]
			if vsw != nil {
				vpcID = tea.StringValue(vsw.VpcId)
				preferredZone = tea.StringValue(vsw.ZoneId)
				vswID := tea.StringValue(vsw.VSwitchId)
				seen[vswID] = true
				vswitches = append(vswitches, AliyunVSwitchInfo{
					VSwitchID:          vswID,
					ZoneID:             preferredZone,
					VpcID:              vpcID,
					AvailableIPCount:   tea.Int64Value(vsw.AvailableIpAddressCount),
					IsPreferredVSwitch: true,
					IsPreferredZone:    true,
				})
			}
		}
	}

	if vpcID == "" && securityGroupID != "" {
		sgReq := &ecs.DescribeSecurityGroupAttributeRequest{
			RegionId:        tea.String(region),
			SecurityGroupId: tea.String(securityGroupID),
		}
		sgResp, err := ecsClient.DescribeSecurityGroupAttribute(sgReq)
		if err == nil && sgResp.Body != nil {
			vpcID = tea.StringValue(sgResp.Body.VpcId)
		}
	}

	if vpcID != "" {
		pageNumber := int32(1)
		pageSize := int32(50)
		for {
			req := &ecs.DescribeVSwitchesRequest{
				RegionId:   tea.String(region),
				VpcId:      tea.String(vpcID),
				PageNumber: tea.Int32(pageNumber),
				PageSize:   tea.Int32(pageSize),
			}
			resp, err := ecsClient.DescribeVSwitches(req)
			if err != nil || resp.Body == nil || resp.Body.VSwitches == nil || len(resp.Body.VSwitches.VSwitch) == 0 {
				break
			}
			for _, vsw := range resp.Body.VSwitches.VSwitch {
				if vsw == nil {
					continue
				}
				vswID := tea.StringValue(vsw.VSwitchId)
				status := tea.StringValue(vsw.Status)
				availIPs := tea.Int64Value(vsw.AvailableIpAddressCount)
				if vswID == "" || seen[vswID] {
					continue
				}
				if status != "" && status != "Available" {
					continue
				}
				if availIPs <= 0 {
					continue
				}
				zoneID := tea.StringValue(vsw.ZoneId)
				seen[vswID] = true
				vswitches = append(vswitches, AliyunVSwitchInfo{
					VSwitchID:          vswID,
					ZoneID:             zoneID,
					VpcID:              tea.StringValue(vsw.VpcId),
					AvailableIPCount:   availIPs,
					IsPreferredVSwitch: vswID == preferredVSwitchID,
					IsPreferredZone:    preferredZone != "" && zoneID == preferredZone,
				})
			}
			total := tea.Int32Value(resp.Body.TotalCount)
			if pageNumber*pageSize >= total || len(resp.Body.VSwitches.VSwitch) < int(pageSize) {
				break
			}
			pageNumber++
		}
	}

	if len(vswitches) == 0 {
		return nil, fmt.Errorf("no available VSwitches found in region %s (preferred VSwitch=%q, SecurityGroup=%q); please check ALIBABA_CLOUD_VSWITCH_ID",
			region, preferredVSwitchID, securityGroupID)
	}

	sort.SliceStable(vswitches, func(i, j int) bool {
		if vswitches[i].IsPreferredVSwitch != vswitches[j].IsPreferredVSwitch {
			return vswitches[i].IsPreferredVSwitch
		}
		if vswitches[i].IsPreferredZone != vswitches[j].IsPreferredZone {
			return vswitches[i].IsPreferredZone
		}
		if vswitches[i].AvailableIPCount != vswitches[j].AvailableIPCount {
			return vswitches[i].AvailableIPCount > vswitches[j].AvailableIPCount
		}
		return vswitches[i].VSwitchID < vswitches[j].VSwitchID
	})
	return vswitches, nil
}

// FetchAliyunX86InstanceTypes queries DescribeInstanceTypes for X86 cloud-disk-compatible instance specs.
func FetchAliyunX86InstanceTypes(ecsClient *ecs.Client, preferredType string) (map[string]AliyunInstanceTypeSpec, error) {
	specs := make(map[string]AliyunInstanceTypeSpec)
	var nextToken string

	for {
		req := &ecs.DescribeInstanceTypesRequest{
			CpuArchitecture: tea.String("X86"),
			MaxResults:      tea.Int64(1600),
		}
		if nextToken != "" {
			req.NextToken = tea.String(nextToken)
		}
		resp, err := ecsClient.DescribeInstanceTypes(req)
		if err != nil {
			return nil, fmt.Errorf("DescribeInstanceTypes failed: %w", err)
		}
		if resp.Body == nil || resp.Body.InstanceTypes == nil || len(resp.Body.InstanceTypes.InstanceType) == 0 {
			break
		}
		for _, it := range resp.Body.InstanceTypes.InstanceType {
			if it == nil {
				continue
			}
			typeID := tea.StringValue(it.InstanceTypeId)
			if typeID == "" {
				continue
			}
			if typeID != preferredType {
				if tea.Int32Value(it.GPUAmount) > 0 {
					continue
				}
				if tea.Int32Value(it.LocalStorageAmount) > 0 {
					continue
				}
			}
			specs[typeID] = AliyunInstanceTypeSpec{
				InstanceType: typeID,
				CPU:          int(tea.Int32Value(it.CpuCoreCount)),
				Memory:       float64(tea.Float32Value(it.MemorySize)),
				Family:       tea.StringValue(it.InstanceTypeFamily),
				FamilyLevel:  tea.StringValue(it.InstanceFamilyLevel),
			}
		}
		nextToken = tea.StringValue(resp.Body.NextToken)
		if nextToken == "" {
			break
		}
	}
	return specs, nil
}

// FetchAliyunAvailableInstanceTypesByZone queries DescribeAvailableResource for in-stock spot instance types across all zones in the region.
func FetchAliyunAvailableInstanceTypesByZone(ecsClient *ecs.Client, region string) (map[string]map[string]string, error) {
	req := &ecs.DescribeAvailableResourceRequest{
		RegionId:            tea.String(region),
		DestinationResource: tea.String("InstanceType"),
		ResourceType:        tea.String("instance"),
		InstanceChargeType:  tea.String("PostPaid"),
		SpotStrategy:        tea.String("SpotWithPriceLimit"),
		IoOptimized:         tea.String("optimized"),
		NetworkCategory:     tea.String("vpc"),
	}
	resp, err := ecsClient.DescribeAvailableResource(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeAvailableResource failed: %w", err)
	}

	zoneStock := make(map[string]map[string]string)
	if resp.Body == nil || resp.Body.AvailableZones == nil {
		return zoneStock, nil
	}
	for _, az := range resp.Body.AvailableZones.AvailableZone {
		if az == nil {
			continue
		}
		zoneID := tea.StringValue(az.ZoneId)
		zoneStatus := tea.StringValue(az.Status)
		zoneCategory := tea.StringValue(az.StatusCategory)
		if zoneID == "" || zoneStatus != "Available" || zoneCategory == "WithoutStock" || zoneCategory == "ClosedWithoutStock" {
			continue
		}
		if az.AvailableResources == nil {
			continue
		}
		for _, ar := range az.AvailableResources.AvailableResource {
			if ar == nil || ar.SupportedResources == nil {
				continue
			}
			for _, sr := range ar.SupportedResources.SupportedResource {
				if sr == nil {
					continue
				}
				instType := tea.StringValue(sr.Value)
				status := tea.StringValue(sr.Status)
				category := tea.StringValue(sr.StatusCategory)
				if instType == "" || status != "Available" {
					continue
				}
				if category != "WithStock" && category != "MoreThanStock" {
					continue
				}
				if zoneStock[zoneID] == nil {
					zoneStock[zoneID] = make(map[string]string)
				}
				zoneStock[zoneID][instType] = category
			}
		}
	}
	return zoneStock, nil
}

// ResolveAliyunSystemDiskCategories returns supported and in-stock system disk categories for a (ZoneId, InstanceType), ordered by cost-effectiveness.
func ResolveAliyunSystemDiskCategories(ecsClient *ecs.Client, region, zoneID, instanceType string) []string {
	req := &ecs.DescribeAvailableResourceRequest{
		RegionId:            tea.String(region),
		ZoneId:              tea.String(zoneID),
		DestinationResource: tea.String("SystemDisk"),
		ResourceType:        tea.String("instance"),
		InstanceChargeType:  tea.String("PostPaid"),
		SpotStrategy:        tea.String("SpotWithPriceLimit"),
		InstanceType:        tea.String(instanceType),
		IoOptimized:         tea.String("optimized"),
		NetworkCategory:     tea.String("vpc"),
	}
	resp, err := ecsClient.DescribeAvailableResource(req)
	var available []string
	if err == nil && resp.Body != nil && resp.Body.AvailableZones != nil {
		for _, az := range resp.Body.AvailableZones.AvailableZone {
			if az == nil || az.AvailableResources == nil {
				continue
			}
			for _, ar := range az.AvailableResources.AvailableResource {
				if ar == nil || ar.SupportedResources == nil {
					continue
				}
				for _, sr := range ar.SupportedResources.SupportedResource {
					if sr == nil {
						continue
					}
					val := tea.StringValue(sr.Value)
					status := tea.StringValue(sr.Status)
					cat := tea.StringValue(sr.StatusCategory)
					if val != "" && status == "Available" && (cat == "WithStock" || cat == "MoreThanStock") {
						available = append(available, val)
					}
				}
			}
		}
	}
	return rankSystemDiskCategories(available, instanceType)
}

func rankSystemDiskCategories(available []string, instanceType string) []string {
	priority := map[string]int{
		"cloud_essd_entry": 1,
		"cloud_efficiency": 2,
		"cloud_auto":       3,
		"cloud_essd":       4,
		"cloud_ssd":        5,
		"cloud":            6,
	}
	if len(available) == 0 {
		if strings.HasPrefix(instanceType, "ecs.u1") || strings.HasPrefix(instanceType, "ecs.e") {
			return []string{"cloud_essd_entry", "cloud_essd", "cloud_auto", "cloud_efficiency"}
		}
		return []string{"cloud_efficiency", "cloud_essd", "cloud_auto", "cloud_ssd"}
	}
	seen := make(map[string]bool, len(available))
	var unique []string
	for _, cat := range available {
		if !seen[cat] {
			seen[cat] = true
			unique = append(unique, cat)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool {
		pi, oki := priority[unique[i]]
		if !oki {
			pi = 99
		}
		pj, okj := priority[unique[j]]
		if !okj {
			pj = 99
		}
		if pi != pj {
			return pi < pj
		}
		return unique[i] < unique[j]
	})
	return unique
}

// SelectAliyunCandidates filters, prices, and ranks available Aliyun spot instance candidates across the VPC's VSwitches.
func SelectAliyunCandidates(ecsClient *ecs.Client, region string, vswitches []AliyunVSwitchInfo, opts CreateSpecOptions, maxprice float64) ([]AliyunCandidate, error) {
	specs, err := FetchAliyunX86InstanceTypes(ecsClient, opts.InstanceType)
	if err != nil {
		return nil, err
	}
	zoneStock, err := FetchAliyunAvailableInstanceTypesByZone(ecsClient, region)
	if err != nil {
		return nil, err
	}

	candidates := buildAliyunCandidates(vswitches, specs, zoneStock, opts)
	if len(candidates) == 0 {
		return nil, nil
	}

	// Enrich top candidates with real-time spot prices via DescribeSpotPriceHistory
	type zoneTypeKey struct {
		zoneID       string
		instanceType string
	}
	priceCache := make(map[zoneTypeKey]float64)
	const maxPriceQueries = 24
	queries := 0
	for i := range candidates {
		key := zoneTypeKey{zoneID: candidates[i].ZoneID, instanceType: candidates[i].InstanceType}
		if price, ok := priceCache[key]; ok {
			candidates[i].SpotPrice = price
			continue
		}
		if queries >= maxPriceQueries {
			continue
		}
		queries++
		price := queryAliyunSpotPrice(ecsClient, region, candidates[i].ZoneID, candidates[i].InstanceType)
		priceCache[key] = price
		candidates[i].SpotPrice = price
	}

	filtered := filterAndSortAliyunCandidates(candidates, maxprice)
	return filtered, nil
}

func queryAliyunSpotPrice(ecsClient *ecs.Client, region, zoneID, instanceType string) float64 {
	req := &ecs.DescribeSpotPriceHistoryRequest{
		RegionId:     tea.String(region),
		ZoneId:       tea.String(zoneID),
		InstanceType: tea.String(instanceType),
		NetworkType:  tea.String("vpc"),
		IoOptimized:  tea.String("optimized"),
		OSType:       tea.String("linux"),
	}
	resp, err := ecsClient.DescribeSpotPriceHistory(req)
	if err != nil || resp.Body == nil || resp.Body.SpotPrices == nil || len(resp.Body.SpotPrices.SpotPriceType) == 0 {
		return 0
	}
	var latestPrice float64
	var latestTime string
	for _, item := range resp.Body.SpotPrices.SpotPriceType {
		if item == nil || item.SpotPrice == nil {
			continue
		}
		ts := tea.StringValue(item.Timestamp)
		p := float64(tea.Float32Value(item.SpotPrice))
		if p > 0 && (latestTime == "" || ts >= latestTime) {
			latestTime = ts
			latestPrice = p
		}
	}
	return latestPrice
}

func buildAliyunCandidates(vswitches []AliyunVSwitchInfo, specs map[string]AliyunInstanceTypeSpec, zoneStock map[string]map[string]string, opts CreateSpecOptions) []AliyunCandidate {
	var candidates []AliyunCandidate
	for _, vsw := range vswitches {
		inStockMap := zoneStock[vsw.ZoneID]
		if len(inStockMap) == 0 {
			continue
		}
		for instType, stockCat := range inStockMap {
			spec, ok := specs[instType]
			if !ok {
				continue
			}
			explicitMatch := opts.InstanceType != "" && strings.EqualFold(instType, opts.InstanceType)
			exactMatch := spec.CPU == opts.CPU && math.Abs(spec.Memory-opts.Memory) < 0.01
			upwardMatch := spec.CPU >= opts.CPU && spec.Memory+0.01 >= opts.Memory &&
				spec.CPU <= maxInt(opts.CPU*4, 4) && spec.Memory <= math.Max(opts.Memory*4, 8.0)

			if !explicitMatch && !exactMatch && !upwardMatch {
				continue
			}
			candidates = append(candidates, AliyunCandidate{
				VSwitchID:        vsw.VSwitchID,
				ZoneID:           vsw.ZoneID,
				InstanceType:     instType,
				CPU:              spec.CPU,
				Memory:           spec.Memory,
				Family:           spec.Family,
				StockCategory:    stockCat,
				ExplicitMatch:    explicitMatch,
				ExactMatch:       exactMatch,
				PreferredVSwitch: vsw.IsPreferredVSwitch,
				PreferredZone:    vsw.IsPreferredZone,
			})
		}
	}
	sortAliyunCandidates(candidates)
	return candidates
}

func filterAndSortAliyunCandidates(candidates []AliyunCandidate, maxprice float64) []AliyunCandidate {
	var withinBudget []AliyunCandidate
	for _, c := range candidates {
		if c.SpotPrice > 0 && c.SpotPrice > maxprice {
			continue
		}
		withinBudget = append(withinBudget, c)
	}
	if len(withinBudget) == 0 {
		withinBudget = candidates
	}
	sortAliyunCandidates(withinBudget)
	return withinBudget
}

func sortAliyunCandidates(candidates []AliyunCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.ExplicitMatch != b.ExplicitMatch {
			return a.ExplicitMatch
		}
		if a.ExactMatch != b.ExactMatch {
			return a.ExactMatch
		}
		if a.PreferredVSwitch != b.PreferredVSwitch {
			return a.PreferredVSwitch
		}
		if a.PreferredZone != b.PreferredZone {
			return a.PreferredZone
		}
		if a.CPU != b.CPU {
			return a.CPU < b.CPU
		}
		if math.Abs(a.Memory-b.Memory) >= 0.01 {
			return a.Memory < b.Memory
		}
		aHasPrice := a.SpotPrice > 0
		bHasPrice := b.SpotPrice > 0
		if aHasPrice && bHasPrice && math.Abs(a.SpotPrice-b.SpotPrice) >= 0.0001 {
			return a.SpotPrice < b.SpotPrice
		}
		fa := aliyunFamilyPriority(a.Family)
		fb := aliyunFamilyPriority(b.Family)
		if fa != fb {
			return fa < fb
		}
		if aHasPrice != bHasPrice {
			return aHasPrice
		}
		if a.StockCategory != b.StockCategory {
			return a.StockCategory == "WithStock"
		}
		if a.InstanceType != b.InstanceType {
			return a.InstanceType < b.InstanceType
		}
		return a.VSwitchID < b.VSwitchID
	})
}

// aliyunFamilyPriority assigns a lower rank number to more cost-effective / entry-level Aliyun ECS instance families.
func aliyunFamilyPriority(family string) int {
	family = strings.ToLower(strings.TrimSpace(family))
	switch {
	case family == "ecs.t6" || family == "ecs.t5" || family == "ecs.s6" || family == "ecs.n4" || family == "ecs.xn4" || family == "ecs.mn4" || family == "ecs.e4":
		return 1
	case strings.HasPrefix(family, "ecs.e") || strings.HasPrefix(family, "ecs.u1") || strings.HasPrefix(family, "ecs.u2"):
		return 2
	case strings.HasPrefix(family, "ecs.c6") || strings.HasPrefix(family, "ecs.g6") || strings.HasPrefix(family, "ecs.r6") ||
		strings.HasPrefix(family, "ecs.ic5") || strings.HasPrefix(family, "ecs.sn1") || strings.HasPrefix(family, "ecs.sn2"):
		return 3
	case strings.HasPrefix(family, "ecs.c7") || strings.HasPrefix(family, "ecs.g7") || strings.HasPrefix(family, "ecs.r7") ||
		strings.HasPrefix(family, "ecs.c8") || strings.HasPrefix(family, "ecs.g8") || strings.HasPrefix(family, "ecs.r8"):
		return 4
	default:
		return 5
	}
}

func isAliyunDiskCategoryError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SystemDisk") ||
		strings.Contains(msg, "InvalidSystemDiskCategory")
}

func isAliyunRetryableCreateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	retryCodes := []string{
		"OperationDenied.NoStock",
		"OperationDenied.NoSpotInventory",
		"OperationDenied.ResourceControl",
		"InstanceType.Offline",
		"InvalidInstanceType",
		"InvalidSystemDiskCategory",
		"SpotPriceLimit",
		"Zone.NotOnSale",
		"ResourceNotAvailable",
		"SoldOut",
	}
	for _, code := range retryCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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

// TencentCandidate represents a ranked Tencent Cloud spot instance creation candidate.
type TencentCandidate struct {
	Zone              string
	InstanceType      string
	InstanceFamily    string
	CPU               int
	Memory            float64
	UnitPriceDiscount float64
	StatusCategory    string
	ExplicitMatch     bool
	ExactMatch        bool
	PreferredZone     bool
}

func dispatchTencent(action, region, hostname string, releasetime int64, maxprice float64, ids []string, specOpts CreateSpecOptions) {
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
		runTencent(cvmClient, region, hostname, password, releasetime, maxprice, specOpts)
	case "list":
		instances, err := FetchTencentInstances(cvmClient, ids)
		if err != nil {
			log.Panicf("%v", err)
		}
		printInstanceTable(instances)
	case "zones":
		zones, err := FetchTencentZones(cvmClient)
		if err != nil {
			log.Panicf("%v", err)
		}
		printZoneTable(zones)
	case "images":
		images, err := FetchTencentPublicImages(cvmClient, specOpts.InstanceType)
		if err != nil {
			log.Panicf("%v", err)
		}
		printImageTable(images)
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

func runTencent(cvmClient *cvm.Client, region, hostname, password string, releasetime int64, maxprice float64, specOpts CreateSpecOptions) {
	preferredZone := strings.TrimSpace(os.Getenv("TENCENTCLOUD_ZONE"))
	subnetID := strings.TrimSpace(os.Getenv("TENCENTCLOUD_SUBNET_ID"))
	allowCrossZone := subnetID == "" || strings.EqualFold(subnetID, "DEFAULT")

	if preferredZone == "" {
		preferredZone = ResolveDefaultTencentZone(cvmClient, region)
	}

	candidates, err := SelectTencentCandidates(cvmClient, preferredZone, allowCrossZone, specOpts, maxprice)
	if err != nil {
		log.Panicf("%v", err)
	}
	if len(candidates) == 0 {
		log.Panicf("no available Tencent Cloud spot instance types in stock for region=%s (preferredZone=%q, target=%dC%.1fG, type=%q, maxprice=%.4f)",
			region, preferredZone, specOpts.CPU, specOpts.Memory, specOpts.InstanceType, maxprice)
	}

	fixedImageID := strings.TrimSpace(os.Getenv("TENCENTCLOUD_IMAGE_ID"))
	imageCache := make(map[string]string)
	var (
		instanceID string
		lastErr    error
	)

	for idx, cand := range candidates {
		imageID := fixedImageID
		if imageID == "" {
			if cached, ok := imageCache[cand.InstanceType]; ok {
				imageID = cached
			} else {
				resolved, imgErr := ResolveTencentPublicImage(cvmClient, cand.InstanceType)
				if imgErr != nil {
					lastErr = imgErr
					log.Printf("[tencent][candidate %d/%d] skipping type=%s in zone=%s (no public image): %v",
						idx+1, len(candidates), cand.InstanceType, cand.Zone, imgErr)
					continue
				}
				imageCache[cand.InstanceType] = resolved
				imageID = resolved
			}
		}

		log.Printf("[tencent][candidate %d/%d] trying zone=%s type=%s (%dC%.1fG) unitPriceDiscount=%.4f stock=%s",
			idx+1, len(candidates), cand.Zone, cand.InstanceType, cand.CPU, cand.Memory, cand.UnitPriceDiscount, cand.StatusCategory)

		createStartTime := time.Now().UTC()
		createdID, createErr := CreateTencentSpotInstance(cvmClient, cand.Zone, cand.InstanceType, imageID, hostname, password, releasetime, maxprice, createStartTime)
		if createErr == nil {
			instanceID = createdID
			break
		}
		lastErr = createErr
		if isTencentRetryableCreateError(createErr) {
			log.Printf("[tencent] candidate %s in %s unavailable, falling back to next candidate: %v",
				cand.InstanceType, cand.Zone, createErr)
			continue
		}
		log.Panicf("%v", createErr)
	}

	if instanceID == "" {
		log.Panicf("all %d Tencent Cloud spot candidates failed in region %s; last error: %v", len(candidates), region, lastErr)
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

// extractTencentZoneOrder parses a Tencent Cloud zone identifier (e.g. "ap-shanghai-8")
// into whether it is a standard regional zone (vs special "-fsi-" financial zone) and its numeric suffix.
func extractTencentZoneOrder(zone string) (isStandard bool, hasNum bool, zoneNum int) {
	zone = strings.TrimSpace(zone)
	lastDash := strings.LastIndex(zone, "-")
	if lastDash <= 0 || lastDash == len(zone)-1 {
		return false, false, 0
	}
	prefix := strings.ToLower(zone[:lastDash])
	suffix := zone[lastDash+1:]
	num, err := strconv.Atoi(suffix)
	if err != nil {
		return false, false, 0
	}
	isSpecial := strings.HasSuffix(prefix, "-fsi") || strings.Contains(prefix, "-fsi-")
	return !isSpecial, true, num
}

// compareTencentZoneDesc returns true if zoneA is newer/higher-priority than zoneB
// (e.g., "ap-shanghai-8" > "ap-shanghai-5" > "ap-shanghai-2" > "ap-shanghai-fsi-3").
func compareTencentZoneDesc(zoneA, zoneB, zoneIDA, zoneIDB string) bool {
	stdA, hasNumA, numA := extractTencentZoneOrder(zoneA)
	stdB, hasNumB, numB := extractTencentZoneOrder(zoneB)
	if stdA != stdB {
		return stdA
	}
	if hasNumA != hasNumB {
		return hasNumA
	}
	if hasNumA && numA != numB {
		return numA > numB
	}
	idA, errA := strconv.Atoi(strings.TrimSpace(zoneIDA))
	idB, errB := strconv.Atoi(strings.TrimSpace(zoneIDB))
	if errA == nil && errB == nil && idA != idB {
		return idA > idB
	}
	return zoneA > zoneB
}

// SelectLatestTencentZone selects the latest AVAILABLE zone from a list of Tencent Cloud ZoneInfo items
// (e.g., "ap-shanghai-8" in ap-shanghai).
func SelectLatestTencentZone(zoneSet []*cvm.ZoneInfo) string {
	type zoneCand struct {
		zone   string
		zoneID string
		avail  bool
	}
	var cands []zoneCand
	for _, z := range zoneSet {
		if z == nil || z.Zone == nil || strings.TrimSpace(*z.Zone) == "" {
			continue
		}
		zone := strings.TrimSpace(*z.Zone)
		zoneID := ""
		if z.ZoneId != nil {
			zoneID = strings.TrimSpace(*z.ZoneId)
		}
		state := "AVAILABLE"
		if z.ZoneState != nil && strings.TrimSpace(*z.ZoneState) != "" {
			state = strings.ToUpper(strings.TrimSpace(*z.ZoneState))
		}
		cands = append(cands, zoneCand{
			zone:   zone,
			zoneID: zoneID,
			avail:  state == "AVAILABLE",
		})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].avail != cands[j].avail {
			return cands[i].avail
		}
		return compareTencentZoneDesc(cands[i].zone, cands[j].zone, cands[i].zoneID, cands[j].zoneID)
	})
	return cands[0].zone
}

// ResolveDefaultTencentZone queries DescribeZones to find the latest available zone in the region (e.g. ap-shanghai-8).
func ResolveDefaultTencentZone(cvmClient *cvm.Client, region string) string {
	req := cvm.NewDescribeZonesRequest()
	resp, err := cvmClient.DescribeZones(req)
	if err != nil || resp.Response == nil || len(resp.Response.ZoneSet) == 0 {
		return ""
	}
	latest := SelectLatestTencentZone(resp.Response.ZoneSet)
	if latest != "" {
		log.Printf("resolved default Tencent Cloud latest zone for region %s: %s", region, latest)
	}
	return latest
}

// FetchTencentZones queries availability zones in the current Tencent Cloud region and marks the default (latest) zone.
func FetchTencentZones(cvmClient *cvm.Client) ([]ZoneSummary, error) {
	req := cvm.NewDescribeZonesRequest()
	resp, err := cvmClient.DescribeZones(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeZones failed: %w", err)
	}
	if resp.Response == nil || len(resp.Response.ZoneSet) == 0 {
		return nil, nil
	}

	defaultZone := strings.TrimSpace(os.Getenv("TENCENTCLOUD_ZONE"))
	if defaultZone == "" {
		defaultZone = SelectLatestTencentZone(resp.Response.ZoneSet)
	}

	var zones []ZoneSummary
	for _, z := range resp.Response.ZoneSet {
		if z == nil || z.Zone == nil || strings.TrimSpace(*z.Zone) == "" {
			continue
		}
		zone := strings.TrimSpace(*z.Zone)
		zoneID := ""
		if z.ZoneId != nil {
			zoneID = strings.TrimSpace(*z.ZoneId)
		}
		name := ""
		if z.ZoneName != nil {
			name = strings.TrimSpace(*z.ZoneName)
		}
		state := ""
		if z.ZoneState != nil {
			state = strings.TrimSpace(*z.ZoneState)
		}
		zones = append(zones, ZoneSummary{
			Zone:      zone,
			ZoneID:    zoneID,
			Name:      name,
			State:     state,
			IsDefault: zone == defaultZone,
		})
	}

	sort.SliceStable(zones, func(i, j int) bool {
		availI := zones[i].State == "AVAILABLE"
		availJ := zones[j].State == "AVAILABLE"
		if availI != availJ {
			return availI
		}
		return compareTencentZoneDesc(zones[i].Zone, zones[j].Zone, zones[i].ZoneID, zones[j].ZoneID)
	})
	return zones, nil
}

func inferLatestTencentZoneFromQuotaItems(items []*cvm.InstanceTypeQuotaItem) string {
	var bestZone string
	for _, item := range items {
		if item == nil || item.Zone == nil {
			continue
		}
		zone := strings.TrimSpace(*item.Zone)
		status := ""
		if item.Status != nil {
			status = *item.Status
		}
		statusCategory := ""
		if item.StatusCategory != nil {
			statusCategory = *item.StatusCategory
		}
		if zone == "" || status != "SELL" || statusCategory == "WithoutStock" {
			continue
		}
		if bestZone == "" || compareTencentZoneDesc(zone, bestZone, "", "") {
			bestZone = zone
		}
	}
	return bestZone
}

// SelectTencentCandidates queries DescribeZoneInstanceConfigInfos for SPOTPAID in-stock X86 instance types and ranks them by cost-effectiveness.
func SelectTencentCandidates(cvmClient *cvm.Client, preferredZone string, allowCrossZone bool, opts CreateSpecOptions, maxprice float64) ([]TencentCandidate, error) {
	req := cvm.NewDescribeZoneInstanceConfigInfosRequest()
	req.Filters = []*cvm.Filter{
		{
			Name:   common.StringPtr("instance-charge-type"),
			Values: []*string{common.StringPtr("SPOTPAID")},
		},
	}
	if preferredZone != "" && !allowCrossZone {
		req.Filters = append(req.Filters, &cvm.Filter{
			Name:   common.StringPtr("zone"),
			Values: []*string{common.StringPtr(preferredZone)},
		})
	}

	resp, err := cvmClient.DescribeZoneInstanceConfigInfos(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeZoneInstanceConfigInfos failed: %w", err)
	}
	if resp.Response == nil || len(resp.Response.InstanceTypeQuotaSet) == 0 {
		return nil, nil
	}
	return buildTencentCandidates(resp.Response.InstanceTypeQuotaSet, preferredZone, allowCrossZone, opts, maxprice), nil
}

func buildTencentCandidates(items []*cvm.InstanceTypeQuotaItem, preferredZone string, allowCrossZone bool, opts CreateSpecOptions, maxprice float64) []TencentCandidate {
	preferredZone = strings.TrimSpace(preferredZone)
	if preferredZone == "" {
		preferredZone = inferLatestTencentZoneFromQuotaItems(items)
	}

	var allCandidates []TencentCandidate
	for _, item := range items {
		if item == nil {
			continue
		}
		zone := ""
		if item.Zone != nil {
			zone = *item.Zone
		}
		instType := ""
		if item.InstanceType != nil {
			instType = *item.InstanceType
		}
		status := ""
		if item.Status != nil {
			status = *item.Status
		}
		statusCategory := ""
		if item.StatusCategory != nil {
			statusCategory = *item.StatusCategory
		}
		if zone == "" || instType == "" || status != "SELL" || statusCategory == "WithoutStock" {
			continue
		}
		if preferredZone != "" && !allowCrossZone && zone != preferredZone {
			continue
		}

		explicitMatch := opts.InstanceType != "" && strings.EqualFold(instType, opts.InstanceType)
		if !explicitMatch {
			if item.Gpu != nil && *item.Gpu > 0 {
				continue
			}
			if item.Fpga != nil && *item.Fpga > 0 {
				continue
			}
			if item.StorageBlockAmount != nil && *item.StorageBlockAmount > 0 {
				continue
			}
			family := ""
			if item.InstanceFamily != nil {
				family = *item.InstanceFamily
			}
			cpuType := ""
			if item.CpuType != nil {
				cpuType = *item.CpuType
			}
			if isTencentARMInstance(family, cpuType) {
				continue
			}
		}

		cpu := 0
		if item.Cpu != nil {
			cpu = int(*item.Cpu)
		}
		memory := 0.0
		if item.Memory != nil {
			memory = float64(*item.Memory)
		}
		exactMatch := cpu == opts.CPU && math.Abs(memory-opts.Memory) < 0.01
		upwardMatch := cpu >= opts.CPU && memory+0.01 >= opts.Memory &&
			cpu <= maxInt(opts.CPU*4, 4) && memory <= math.Max(opts.Memory*4, 8.0)
		if !explicitMatch && !exactMatch && !upwardMatch {
			continue
		}

		unitPriceDiscount := 0.0
		if item.Price != nil && item.Price.UnitPriceDiscount != nil {
			unitPriceDiscount = *item.Price.UnitPriceDiscount
		}
		family := ""
		if item.InstanceFamily != nil {
			family = *item.InstanceFamily
		}

		allCandidates = append(allCandidates, TencentCandidate{
			Zone:              zone,
			InstanceType:      instType,
			InstanceFamily:    family,
			CPU:               cpu,
			Memory:            memory,
			UnitPriceDiscount: unitPriceDiscount,
			StatusCategory:    statusCategory,
			ExplicitMatch:     explicitMatch,
			ExactMatch:        exactMatch,
			PreferredZone:     preferredZone != "" && zone == preferredZone,
		})
	}

	var withinBudget []TencentCandidate
	for _, c := range allCandidates {
		if c.UnitPriceDiscount > 0 && c.UnitPriceDiscount > maxprice {
			continue
		}
		withinBudget = append(withinBudget, c)
	}
	if len(withinBudget) == 0 {
		withinBudget = allCandidates
	}
	sortTencentCandidates(withinBudget)
	return withinBudget
}

func sortTencentCandidates(candidates []TencentCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.ExplicitMatch != b.ExplicitMatch {
			return a.ExplicitMatch
		}
		if a.ExactMatch != b.ExactMatch {
			return a.ExactMatch
		}
		if a.PreferredZone != b.PreferredZone {
			return a.PreferredZone
		}
		if a.CPU != b.CPU {
			return a.CPU < b.CPU
		}
		if math.Abs(a.Memory-b.Memory) >= 0.01 {
			return a.Memory < b.Memory
		}
		aHasPrice := a.UnitPriceDiscount > 0
		bHasPrice := b.UnitPriceDiscount > 0
		if aHasPrice && bHasPrice && math.Abs(a.UnitPriceDiscount-b.UnitPriceDiscount) >= 0.0001 {
			return a.UnitPriceDiscount < b.UnitPriceDiscount
		}
		if aHasPrice != bHasPrice {
			return aHasPrice
		}
		fa := tencentFamilyPriority(a.InstanceFamily)
		fb := tencentFamilyPriority(b.InstanceFamily)
		if fa != fb {
			return fa < fb
		}
		sa := tencentStockPriority(a.StatusCategory)
		sb := tencentStockPriority(b.StatusCategory)
		if sa != sb {
			return sa < sb
		}
		if a.Zone != b.Zone {
			return compareTencentZoneDesc(a.Zone, b.Zone, "", "")
		}
		return a.InstanceType < b.InstanceType
	})
}

func isTencentARMInstance(family, cpuType string) bool {
	famUpper := strings.ToUpper(strings.TrimSpace(family))
	cpuUpper := strings.ToUpper(strings.TrimSpace(cpuType))
	if strings.HasPrefix(famUpper, "SR") || famUpper == "SA3K" || famUpper == "S6M" {
		return true
	}
	if strings.Contains(cpuUpper, "ARM") || strings.Contains(cpuUpper, "AMPERE") || strings.Contains(cpuUpper, "KUNPENG") {
		return true
	}
	return false
}

func tencentFamilyPriority(family string) int {
	famUpper := strings.ToUpper(strings.TrimSpace(family))
	switch famUpper {
	case "SA2", "SA3", "S5SE", "S2", "S3", "S4", "S5":
		return 1
	case "SA4", "SA5", "S6", "S7", "S8":
		return 2
	case "MA2", "MA3", "M2", "M3", "M4", "M5", "M6", "C2", "C3", "C4", "C5", "C6":
		return 3
	default:
		return 4
	}
}

func tencentStockPriority(statusCategory string) int {
	switch statusCategory {
	case "EnoughStock":
		return 1
	case "NormalStock":
		return 2
	case "UnderStock":
		return 3
	default:
		return 4
	}
}

func isTencentRetryableCreateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	retryCodes := []string{
		"ResourcesSoldOut",
		"ResourceInsufficient",
		"ZoneNotSupported",
		"InstanceTypeNotSupport",
		"SoldOut",
		"NoStock",
		"InsufficientSpotInstanceStock",
		"InvalidParameterValue.InstanceType",
		"InvalidParameterValue.SystemDiskType",
		"InvalidParameterValue.Zone",
		"UnsupportedOperation",
	}
	for _, code := range retryCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

func convertAndRankTencentImages(imageSet []*cvm.Image, requireAlmaLinux bool) []PublicImageSummary {
	var result []PublicImageSummary
	for _, img := range imageSet {
		if img == nil || img.ImageId == nil || strings.TrimSpace(*img.ImageId) == "" {
			continue
		}
		if img.ImageState != nil && *img.ImageState != "" && *img.ImageState != "NORMAL" {
			continue
		}
		arch := ""
		if img.Architecture != nil {
			arch = strings.TrimSpace(*img.Architecture)
		}
		archLower := strings.ToLower(arch)
		if archLower == "arm" || archLower == "arm64" || archLower == "i386" {
			continue
		}
		imageID := strings.TrimSpace(*img.ImageId)
		imageName := ""
		if img.ImageName != nil {
			imageName = strings.TrimSpace(*img.ImageName)
		}
		osName := ""
		if img.OsName != nil {
			osName = strings.TrimSpace(*img.OsName)
		}
		platform := ""
		if img.Platform != nil {
			platform = strings.TrimSpace(*img.Platform)
		}
		if requireAlmaLinux && !isAlmaLinuxImage(platform, osName, imageName, imageID) {
			continue
		}
		sizeGiB := int64(0)
		if img.ImageSize != nil {
			sizeGiB = *img.ImageSize
		}
		createdTime := ""
		if img.CreatedTime != nil {
			createdTime = strings.TrimSpace(*img.CreatedTime)
		}
		result = append(result, PublicImageSummary{
			ImageID:     imageID,
			ImageName:   imageName,
			OSName:      osName,
			Platform:    platform,
			Arch:        arch,
			SizeGiB:     sizeGiB,
			CreatedTime: createdTime,
		})
	}
	sortPublicImagesDesc(result)
	return result
}

// FetchTencentPublicImages queries Tencent Cloud public x86_64 AlmaLinux images and ranks the latest version (e.g. 10.x) first.
func FetchTencentPublicImages(cvmClient *cvm.Client, instanceType string) ([]PublicImageSummary, error) {
	request := cvm.NewDescribeImagesRequest()
	if strings.TrimSpace(instanceType) != "" {
		request.InstanceType = common.StringPtr(strings.TrimSpace(instanceType))
	}
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
	request.Limit = common.Uint64Ptr(100)

	response, err := cvmClient.DescribeImages(request)
	if err != nil {
		return nil, fmt.Errorf("DescribeImages failed: %w", err)
	}
	if response.Response != nil && len(response.Response.ImageSet) > 0 {
		images := convertAndRankTencentImages(response.Response.ImageSet, true)
		if len(images) > 0 {
			return images, nil
		}
	}

	// Fallback: query PUBLIC_IMAGE without platform filter and filter for AlmaLinux first
	fallbackReq := cvm.NewDescribeImagesRequest()
	if strings.TrimSpace(instanceType) != "" {
		fallbackReq.InstanceType = common.StringPtr(strings.TrimSpace(instanceType))
	}
	fallbackReq.Filters = []*cvm.Filter{
		{
			Name:   common.StringPtr("image-type"),
			Values: []*string{common.StringPtr("PUBLIC_IMAGE")},
		},
	}
	fallbackReq.Limit = common.Uint64Ptr(100)
	fallbackResp, err := cvmClient.DescribeImages(fallbackReq)
	if err != nil {
		return nil, fmt.Errorf("DescribeImages fallback failed: %w", err)
	}
	if fallbackResp.Response == nil || len(fallbackResp.Response.ImageSet) == 0 {
		return nil, nil
	}
	almaImages := convertAndRankTencentImages(fallbackResp.Response.ImageSet, true)
	if len(almaImages) > 0 {
		return almaImages, nil
	}
	return convertAndRankTencentImages(fallbackResp.Response.ImageSet, false), nil
}

// ResolveTencentPublicImage queries the latest supported public AlmaLinux image (e.g. AlmaLinux 10.x) for the instance type.
func ResolveTencentPublicImage(cvmClient *cvm.Client, instanceType string) (string, error) {
	images, err := FetchTencentPublicImages(cvmClient, instanceType)
	if err != nil {
		return "", err
	}
	if len(images) > 0 {
		chosen := images[0]
		log.Printf("resolved latest Tencent Cloud public image: %s (%s)", chosen.ImageID, chosen.OSName)
		return chosen.ImageID, nil
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

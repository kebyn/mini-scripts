package main

import (
	"errors"
	"testing"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
)

func TestResolveCreateSpecOptions(t *testing.T) {
	t.Setenv("ALIBABA_CLOUD_CPU", "2")
	t.Setenv("ALIBABA_CLOUD_MEMORY", "4")
	t.Setenv("ALIBABA_CLOUD_INSTANCE_TYPE", "ecs.e-c1m2.large")

	// When CLI flags are not explicitly set, environment variables take effect
	opts := resolveCreateSpecOptions("aliyun", 1, 1, "", map[string]bool{})
	if opts.CPU != 2 || opts.Memory != 4 || opts.InstanceType != "ecs.e-c1m2.large" {
		t.Fatalf("unexpected opts from env: %+v", opts)
	}

	// When CLI flags are explicitly set, they override environment variables
	optsExplicit := resolveCreateSpecOptions("aliyun", 1, 1, "ecs.t6-c1m1.large", map[string]bool{
		"cpu":    true,
		"memory": true,
		"type":   true,
	})
	if optsExplicit.CPU != 1 || optsExplicit.Memory != 1 || optsExplicit.InstanceType != "ecs.t6-c1m1.large" {
		t.Fatalf("unexpected opts from explicit flags: %+v", optsExplicit)
	}
}

func TestBuildAndSortAliyunCandidates(t *testing.T) {
	vswitches := []AliyunVSwitchInfo{
		{
			VSwitchID:          "vsw-pref",
			ZoneID:             "cn-shanghai-b",
			VpcID:              "vpc-1",
			AvailableIPCount:   100,
			IsPreferredVSwitch: true,
			IsPreferredZone:    true,
		},
		{
			VSwitchID:          "vsw-other",
			ZoneID:             "cn-shanghai-e",
			VpcID:              "vpc-1",
			AvailableIPCount:   200,
			IsPreferredVSwitch: false,
			IsPreferredZone:    false,
		},
	}

	specs := map[string]AliyunInstanceTypeSpec{
		"ecs.t6-c1m1.large":  {InstanceType: "ecs.t6-c1m1.large", CPU: 1, Memory: 1.0, Family: "ecs.t6"},
		"ecs.n4.small":       {InstanceType: "ecs.n4.small", CPU: 1, Memory: 1.0, Family: "ecs.n4"},
		"ecs.g7.large":       {InstanceType: "ecs.g7.large", CPU: 1, Memory: 1.0, Family: "ecs.g7"},
		"ecs.u1-c1m1.large":  {InstanceType: "ecs.u1-c1m1.large", CPU: 2, Memory: 2.0, Family: "ecs.u1"},
		"ecs.c6.2xlarge":     {InstanceType: "ecs.c6.2xlarge", CPU: 8, Memory: 16.0, Family: "ecs.c6"}, // exceeds upward fallback limit for 1C1G
	}

	zoneStock := map[string]map[string]string{
		"cn-shanghai-b": {
			"ecs.g7.large":      "WithStock",
			"ecs.t6-c1m1.large": "WithStock",
			"ecs.u1-c1m1.large": "WithStock",
		},
		"cn-shanghai-e": {
			"ecs.n4.small":      "WithStock",
			"ecs.u1-c1m1.large": "WithStock",
			"ecs.c6.2xlarge":    "WithStock",
		},
	}

	opts := CreateSpecOptions{CPU: 1, Memory: 1.0}
	candidates := buildAliyunCandidates(vswitches, specs, zoneStock, opts)
	if len(candidates) != 5 {
		t.Fatalf("expected 5 candidates (excluding 8C16G), got %d", len(candidates))
	}

	// Simulate spot prices returned by DescribeSpotPriceHistory
	for i := range candidates {
		switch candidates[i].InstanceType {
		case "ecs.t6-c1m1.large":
			candidates[i].SpotPrice = 0.035
		case "ecs.g7.large":
			candidates[i].SpotPrice = 0.120
		case "ecs.n4.small":
			candidates[i].SpotPrice = 0.028
		case "ecs.u1-c1m1.large":
			candidates[i].SpotPrice = 0.080
		}
	}

	sorted := filterAndSortAliyunCandidates(candidates, 0.50)

	// 1st should be exact 1C1G in preferred VSwitch with lowest spot price (ecs.t6-c1m1.large)
	if sorted[0].InstanceType != "ecs.t6-c1m1.large" || sorted[0].ZoneID != "cn-shanghai-b" {
		t.Fatalf("expected first candidate ecs.t6-c1m1.large in cn-shanghai-b, got %+v", sorted[0])
	}
	// 2nd should be exact 1C1G in preferred VSwitch (ecs.g7.large)
	if sorted[1].InstanceType != "ecs.g7.large" || sorted[1].ZoneID != "cn-shanghai-b" {
		t.Fatalf("expected second candidate ecs.g7.large in cn-shanghai-b, got %+v", sorted[1])
	}
	// 3rd should be exact 1C1G in fallback zone cn-shanghai-e (ecs.n4.small)
	if sorted[2].InstanceType != "ecs.n4.small" || sorted[2].ZoneID != "cn-shanghai-e" {
		t.Fatalf("expected third candidate ecs.n4.small in cn-shanghai-e, got %+v", sorted[2])
	}
	// 4th & 5th should be upward fallback 2C2G (preferred zone first, then fallback zone)
	if sorted[3].InstanceType != "ecs.u1-c1m1.large" || sorted[3].ZoneID != "cn-shanghai-b" {
		t.Fatalf("expected fourth candidate ecs.u1-c1m1.large in cn-shanghai-b, got %+v", sorted[3])
	}
	if sorted[4].InstanceType != "ecs.u1-c1m1.large" || sorted[4].ZoneID != "cn-shanghai-e" {
		t.Fatalf("expected fifth candidate ecs.u1-c1m1.large in cn-shanghai-e, got %+v", sorted[4])
	}
}

func TestRankSystemDiskCategories(t *testing.T) {
	got := rankSystemDiskCategories([]string{"cloud_ssd", "cloud_essd", "cloud_essd_entry", "cloud_efficiency"}, "ecs.u1-c1m1.large")
	want := []string{"cloud_essd_entry", "cloud_efficiency", "cloud_essd", "cloud_ssd"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}

func TestBuildAndSortTencentCandidates(t *testing.T) {
	items := []*cvm.InstanceTypeQuotaItem{
		{
			Zone:           common.StringPtr("ap-shanghai-2"),
			InstanceType:   common.StringPtr("S5.SMALL1"),
			InstanceFamily: common.StringPtr("S5"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("NormalStock"),
			CpuType:        common.StringPtr("Intel Xeon Cascade Lake"),
			Price: &cvm.ItemPrice{
				UnitPriceDiscount: common.Float64Ptr(0.06),
			},
		},
		{
			Zone:           common.StringPtr("ap-shanghai-2"),
			InstanceType:   common.StringPtr("SA2.SMALL1"),
			InstanceFamily: common.StringPtr("SA2"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("AMD EPYC Rome"),
			Price: &cvm.ItemPrice{
				UnitPriceDiscount: common.Float64Ptr(0.04),
			},
		},
		{
			Zone:           common.StringPtr("ap-shanghai-2"),
			InstanceType:   common.StringPtr("SR1.SMALL1"), // ARM - should be excluded
			InstanceFamily: common.StringPtr("SR1"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("Ampere Altra"),
			Price: &cvm.ItemPrice{
				UnitPriceDiscount: common.Float64Ptr(0.02),
			},
		},
		{
			Zone:           common.StringPtr("ap-shanghai-5"),
			InstanceType:   common.StringPtr("SA2.SMALL1"),
			InstanceFamily: common.StringPtr("SA2"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("AMD EPYC Rome"),
			Price: &cvm.ItemPrice{
				UnitPriceDiscount: common.Float64Ptr(0.03),
			},
		},
		{
			Zone:           common.StringPtr("ap-shanghai-2"),
			InstanceType:   common.StringPtr("SA2.MEDIUM2"),
			InstanceFamily: common.StringPtr("SA2"),
			Cpu:            common.Int64Ptr(2),
			Memory:         common.Int64Ptr(2),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("AMD EPYC Rome"),
			Price: &cvm.ItemPrice{
				UnitPriceDiscount: common.Float64Ptr(0.08),
			},
		},
	}

	opts := CreateSpecOptions{CPU: 1, Memory: 1.0}
	cands := buildTencentCandidates(items, "ap-shanghai-2", true, opts, 0.50)
	if len(cands) != 4 {
		t.Fatalf("expected 4 candidates (ARM excluded), got %d", len(cands))
	}
	if cands[0].InstanceType != "SA2.SMALL1" || cands[0].Zone != "ap-shanghai-2" {
		t.Fatalf("expected cheapest 1C1G in preferred zone first (SA2.SMALL1 in ap-shanghai-2), got %+v", cands[0])
	}
	if cands[1].InstanceType != "S5.SMALL1" || cands[1].Zone != "ap-shanghai-2" {
		t.Fatalf("expected second 1C1G in preferred zone (S5.SMALL1 in ap-shanghai-2), got %+v", cands[1])
	}
	if cands[2].InstanceType != "SA2.SMALL1" || cands[2].Zone != "ap-shanghai-5" {
		t.Fatalf("expected fallback zone 1C1G third (SA2.SMALL1 in ap-shanghai-5), got %+v", cands[2])
	}
	if cands[3].InstanceType != "SA2.MEDIUM2" {
		t.Fatalf("expected upward fallback 2C2G fourth, got %+v", cands[3])
	}
}

func TestRetryableCreateErrors(t *testing.T) {
	aliyunNoStockErr := errors.New(`SDKError: StatusCode: 403 Code: OperationDenied.NoStock Message: The requested resource is sold out in the specified zone`)
	if !isAliyunRetryableCreateError(aliyunNoStockErr) {
		t.Fatalf("expected OperationDenied.NoStock to be retryable")
	}

	aliyunAuthErr := errors.New(`SDKError: StatusCode: 404 Code: InvalidAccessKeyId.NotFound`)
	if isAliyunRetryableCreateError(aliyunAuthErr) {
		t.Fatalf("expected InvalidAccessKeyId.NotFound not to be retryable")
	}

	tencentSoldOutErr := errors.New(`[TencentCloudSDKError] Code=ResourcesSoldOut.SpecifiedInstanceType, Message=The specified instance type is sold out`)
	if !isTencentRetryableCreateError(tencentSoldOutErr) {
		t.Fatalf("expected ResourcesSoldOut.SpecifiedInstanceType to be retryable")
	}
}

func TestCalcCreateAndExtendReleaseTime(t *testing.T) {
	fallback := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	releaseAt := calcCreateReleaseTime("2026-11-11T09:10:00Z", fallback, 1)
	expected := time.Date(2026, 11, 11, 10, 8, 0, 0, time.UTC)
	if !releaseAt.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, releaseAt)
	}
}

func TestSelectLatestTencentZoneAndDefaultCandidate(t *testing.T) {
	zoneSet := []*cvm.ZoneInfo{
		{
			Zone:      common.StringPtr("ap-shanghai-2"),
			ZoneId:    common.StringPtr("200002"),
			ZoneName:  common.StringPtr("上海二区"),
			ZoneState: common.StringPtr("AVAILABLE"),
		},
		{
			Zone:      common.StringPtr("ap-shanghai-5"),
			ZoneId:    common.StringPtr("200005"),
			ZoneName:  common.StringPtr("上海五区"),
			ZoneState: common.StringPtr("AVAILABLE"),
		},
		{
			Zone:      common.StringPtr("ap-shanghai-8"),
			ZoneId:    common.StringPtr("200008"),
			ZoneName:  common.StringPtr("上海八区"),
			ZoneState: common.StringPtr("AVAILABLE"),
		},
		{
			Zone:      common.StringPtr("ap-shanghai-fsi-3"),
			ZoneId:    common.StringPtr("200099"),
			ZoneName:  common.StringPtr("上海金融三区"),
			ZoneState: common.StringPtr("AVAILABLE"),
		},
		{
			Zone:      common.StringPtr("ap-shanghai-9"),
			ZoneId:    common.StringPtr("200009"),
			ZoneName:  common.StringPtr("上海九区"),
			ZoneState: common.StringPtr("UNAVAILABLE"),
		},
	}

	latestZone := SelectLatestTencentZone(zoneSet)
	if latestZone != "ap-shanghai-8" {
		t.Fatalf("expected latest available zone ap-shanghai-8, got %q", latestZone)
	}

	// Verify buildTencentCandidates defaults to latest zone (ap-shanghai-8) when preferredZone is empty
	items := []*cvm.InstanceTypeQuotaItem{
		{
			Zone:           common.StringPtr("ap-shanghai-2"),
			InstanceType:   common.StringPtr("SA2.SMALL1"),
			InstanceFamily: common.StringPtr("SA2"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("AMD EPYC Rome"),
			Price:          &cvm.ItemPrice{UnitPriceDiscount: common.Float64Ptr(0.03)},
		},
		{
			Zone:           common.StringPtr("ap-shanghai-8"),
			InstanceType:   common.StringPtr("SA2.SMALL1"),
			InstanceFamily: common.StringPtr("SA2"),
			Cpu:            common.Int64Ptr(1),
			Memory:         common.Int64Ptr(1),
			Status:         common.StringPtr("SELL"),
			StatusCategory: common.StringPtr("EnoughStock"),
			CpuType:        common.StringPtr("AMD EPYC Rome"),
			Price:          &cvm.ItemPrice{UnitPriceDiscount: common.Float64Ptr(0.04)},
		},
	}

	opts := CreateSpecOptions{CPU: 1, Memory: 1.0}
	cands := buildTencentCandidates(items, "", true, opts, 0.50)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if cands[0].Zone != "ap-shanghai-8" || !cands[0].PreferredZone {
		t.Fatalf("expected ap-shanghai-8 as default preferred zone first, got %+v", cands[0])
	}
}

func TestSelectLatestAlmaLinuxImage(t *testing.T) {
	imageSet := []*cvm.Image{
		{
			ImageId:      common.StringPtr("img-alma810"),
			ImageName:    common.StringPtr("AlmaLinux 8.10 64bit"),
			OsName:       common.StringPtr("AlmaLinux 8.10 64bit"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("x86_64"),
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2024-06-01T00:00:00Z"),
		},
		{
			ImageId:      common.StringPtr("img-alma95"),
			ImageName:    common.StringPtr("AlmaLinux Server 9.5 64bit"),
			OsName:       common.StringPtr("AlmaLinux Server 9.5 64bit"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("x86_64"),
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2025-05-01T00:00:00Z"),
		},
		{
			ImageId:      common.StringPtr("img-alma100"),
			ImageName:    common.StringPtr("AlmaLinux Server 10.0 64bit"),
			OsName:       common.StringPtr("AlmaLinux Server 10.0 64bit"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("x86_64"),
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2025-08-01T00:00:00Z"),
		},
		{
			ImageId:      common.StringPtr("img-alma101-old"),
			ImageName:    common.StringPtr("AlmaLinux 10.1 64位"),
			OsName:       common.StringPtr("AlmaLinux 10.1 64位"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("x86_64"),
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2025-11-01T00:00:00Z"),
		},
		{
			ImageId:      common.StringPtr("img-alma101-new"),
			ImageName:    common.StringPtr("AlmaLinux 10.1 64位"),
			OsName:       common.StringPtr("AlmaLinux 10.1 64位"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("x86_64"),
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2026-02-01T00:00:00Z"),
		},
		{
			ImageId:      common.StringPtr("img-alma102-arm"),
			ImageName:    common.StringPtr("AlmaLinux 10.2 ARM64"),
			OsName:       common.StringPtr("AlmaLinux 10.2 ARM64"),
			Platform:     common.StringPtr("AlmaLinux"),
			Architecture: common.StringPtr("arm"), // ARM should be excluded
			ImageState:   common.StringPtr("NORMAL"),
			CreatedTime:  common.StringPtr("2026-03-01T00:00:00Z"),
		},
	}

	ranked := convertAndRankTencentImages(imageSet, true)
	if len(ranked) != 5 {
		t.Fatalf("expected 5 x86_64 images (ARM excluded), got %d", len(ranked))
	}
	if ranked[0].ImageID != "img-alma101-new" || !ranked[0].IsDefault {
		t.Fatalf("expected newest AlmaLinux 10.1 image first (img-alma101-new), got %+v", ranked[0])
	}
	if ranked[1].ImageID != "img-alma101-old" {
		t.Fatalf("expected older AlmaLinux 10.1 image second (img-alma101-old), got %+v", ranked[1])
	}
	if ranked[2].ImageID != "img-alma100" {
		t.Fatalf("expected AlmaLinux 10.0 third (img-alma100), got %+v", ranked[2])
	}
	if ranked[3].ImageID != "img-alma95" {
		t.Fatalf("expected AlmaLinux 9.5 fourth (img-alma95), got %+v", ranked[3])
	}
	if ranked[4].ImageID != "img-alma810" {
		t.Fatalf("expected AlmaLinux 8.10 fifth (img-alma810), got %+v", ranked[4])
	}
}

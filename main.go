package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
)

const (
	ipInfoAPI1 = "https://ipinfo.io"
	ipInfoAPI2 = "https://api.ipify.org?format=json"
)

var ipInfoAPIs = [...]string{
	ipInfoAPI1,
	ipInfoAPI2,
}

var (
	apiToken    = os.Getenv("APITOKEN")
	domain      = os.Getenv("DOMAIN")
	prefix      = os.Getenv("PREFIX")
	segment     = os.Getenv("SEGMENT")
	period, _   = strconv.ParseUint(os.Getenv("PERIOD"), 10, 64)
	zoneID      string
	recordID    string
	subDomain   string
	fullDomain  string
	currentZone *cloudflare.Zone
)

func main() {
	// 检查环境变量
	if apiToken == "" || domain == "" || prefix == "" {
		fmt.Println("请设置必要的环境变量: APITOKEN, DOMAIN, PREFIX, PERIOD")
		os.Exit(1)
	}
	if period == 0 {
		period = 60
	}

	subDomain = prefix
	if segment != "" {
		subDomain += "." + segment
	}
	fullDomain = subDomain + "." + domain
	fmt.Println("域名:", fullDomain)

	// 设置 Cloudflare API 密钥
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		fmt.Println("Cloudflare API 初始化失败:", err)
		os.Exit(1)
	}

	// 获取所有 Zones，根据顶级域名进行过滤
	zones, err := api.ListZones(context.Background(), domain)
	if err != nil {
		fmt.Println("获取 Cloudflare Zones 失败:", err)
		os.Exit(1)
	}
	// 寻找匹配的 Zone
	for _, z := range zones {
		if strings.HasSuffix(domain, z.Name) {
			currentZone = &z
			zoneID = z.ID
			fmt.Println("获取ZoneID成功:", zoneID)
			break
		}
	}
	if currentZone == nil {
		fmt.Printf("找不到与域名 %s 匹配的 Cloudflare Zone\n", domain)
		os.Exit(1)
	}
	// 定期执行更新操作
	for {
		currentIP, err := getCurrentIP()
		if err != nil {
			fmt.Println("获取当前外网地址失败:", err)
			continue
		}
		fmt.Println("获取公网IP成功:", currentIP)
		comment := ""
		// 获取当前 DNS 记录
		dnsRecords, err := getDNSRecord(api, zoneID, fullDomain, comment)
		if err != nil {
			fmt.Println("获取 DNS 记录失败:", err)
			continue
		}
		dnsRecord := cloudflare.DNSRecord{}
		if len(dnsRecords) == 0 {
			dnsRecord, err := createDNSRecord(api, zoneID, subDomain, currentIP)
			if err != nil {
				fmt.Println("创建 DNS 记录失败:", err)
				continue
			}
			fmt.Println("创建 DNS 记录成功:", dnsRecord.Name, currentIP)
		} else {
			dnsRecord = dnsRecords[0]
		}

		// 如果外网地址与 DNS 记录不一样，则更新 DNS 记录
		if currentIP != dnsRecord.Content && dnsRecord.Content != "" {
			fmt.Println("公网IP变化，更新DNS记录:", dnsRecord.Content+" => "+currentIP)
			err := updateDNSRecord(api, dnsRecord.ID, zoneID, subDomain, currentIP)
			if err != nil {
				fmt.Println("更新 DNS 记录失败:", err)
			} else {
				fmt.Println("DNS 记录已更新:", dnsRecord.Name, currentIP)
			}
		} else {
			fmt.Println("公网地址与 DNS 记录一致，无需更新")
		}
		time.Sleep(time.Duration(period) * time.Second)
	}
}

var lastSuccessfulAPI string

// getCurrentIP 获取当前外网地址
func getCurrentIP() (string, error) {
	for _, api := range ipInfoAPIs {
		if lastSuccessfulAPI != "" && api != lastSuccessfulAPI {
			continue
		}
		resp, err := http.Get(api)
		if err != nil {
			// 记录错误日志
			fmt.Printf("获取外网地址失败：%s\n", err)
			continue
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			// 记录错误日志
			fmt.Printf("读取响应体失败：%s\n", err)
			continue
		}
		var ipInfo map[string]interface{}
		err = json.Unmarshal(body, &ipInfo)
		if err != nil {
			// 记录错误日志
			fmt.Printf("解析 JSON 失败：%s\n", err)
			continue
		}
		// 如果成功获取到 IP 地址，返回它
		if ip, ok := ipInfo["ip"].(string); ok {
			lastSuccessfulAPI = api
			return ip, nil
		}
	}
	lastSuccessfulAPI = ""
	// 如果所有 API 都失败，返回错误
	return "", fmt.Errorf("所有 API 获取外网地址失败")
}

// getDNSRecord 获取指定的 Cloudflare DNS 记录
func getDNSRecord(api *cloudflare.API, zoneID, name string, comment string) ([]cloudflare.DNSRecord, error) {

	// 定义 ListDNSRecordsParams 参数
	params := cloudflare.ListDNSRecordsParams{
		Name:    name,
		Type:    "A",
		Comment: comment,
	}

	// 定义 ResourceContainer
	rc := &cloudflare.ResourceContainer{Identifier: zoneID}

	// 调用 ListDNSRecords 函数
	records, _, err := api.ListDNSRecords(context.Background(), rc, params)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// 新建DNS 记录
func createDNSRecord(api *cloudflare.API, zoneID, subdomain, content string) (cloudflare.DNSRecord, error) {
	createdRecord := cloudflare.CreateDNSRecordParams{
		Name:    subdomain,
		Content: content,
		Type:    "A",
		Proxied: &[]bool{false}[0],
		ZoneID:  zoneID,
	}

	rc := &cloudflare.ResourceContainer{Identifier: zoneID}

	dnsRecord, err := api.CreateDNSRecord(context.Background(), rc, createdRecord)

	return dnsRecord, err
}

// updateDNSRecord 更新指定的 Cloudflare DNS 记录
func updateDNSRecord(api *cloudflare.API, recordID, zoneID, subdomain, content string) error {
	// 定义更新的 DNS 记录
	updatedRecord := cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Name:    subdomain,
		Content: content,
		Type:    "A", // 假设类型是 A 记录，根据实际情况修改
	}

	// 定义 ResourceContainer
	rc := &cloudflare.ResourceContainer{Identifier: zoneID}

	// 调用 UpdateDNSRecord 函数
	_, err := api.UpdateDNSRecord(context.Background(), rc, updatedRecord)
	if err != nil {
		return err
	}

	return nil
}

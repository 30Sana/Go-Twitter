package main

import (
	"30SanaPkg/webhook"
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var procSetConsoleTitleW = kernel32.NewProc("SetConsoleTitleW")

func setConsoleTitle(title string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(titlePtr)))
}

func main() {
	proxies, err := loadProxies("data/proxies.txt")
	if err != nil {
		fmt.Println("Error loading proxies:", err)
		return
	}

	targets, err := loadTargets("data/targets.txt")
	if err != nil {
		fmt.Println("Error loading targets:", err)
		return
	}

	headers := map[string]string{
		//"Kdt":                                "tP6jupowsB8ZGMA2LMghaMBPIXJQWEUWbvXlRear",
		// "X-Twitter-Client-Deviceid": "00000000-0000-0000-0000-000000000000",
		// "X-Client-Uuid":             "799DBDEA-ED6D-4E88-A0CC-C86FC260006E",
		// "X-B3-Traceid":                       "3dc8845fbb29ec5d",
		"Host":                               "api-0-4-7.twitter.com",
		"Accept":                             "application/json",
		"X-Twitter-Client-Version":           "9.44",
		"X-Twitter-Client-Language":          "en",
		"Accept-Language":                    "en",
		"User-Agent":                         "Twitter-iPhone/9.44 iOS/14.8.1 (Apple;iPhone10,4;;;;;1;2017)",
		"X-Twitter-Client-Limit-Ad-Tracking": "1",
		"X-Twitter-Api-Version":              "5",
		"X-Twitter-Client":                   "Twitter-iPhone",
	}
	var data = []byte(nil)

	numConcurrentRequests := 300 // Stable / Good speed --> 750 / 5
	batchSize := 1
	totalRequestLimit := 1000000 // 1000000

	var requestCount int
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(numConcurrentRequests)

	startTime := time.Now()
	lastUpdateTime := startTime
	lastRequestCount := 0

	for i := 0; i < numConcurrentRequests; i++ {
		go func() {
			defer wg.Done()

			for {
				targetBatch := getNextTargets(&targets, batchSize)
				if len(targetBatch) == 0 {
					return // No more targets to process
				}

				proxy := getRandomProxy(proxies)
				client := createHTTPClient(proxy)
				for _, target := range targetBatch {
					mu.Lock()
					if requestCount >= totalRequestLimit {
						mu.Unlock()
						return // Stop processing requests
					}
					requestCount++
					mu.Unlock()

					targetURL := "https://api-0-4-7.twitter.com:443/i/users/username_available.json?context=signup&custom=1&send_error_codes=1&suggest=1&username=" + target
					response := httpRequest(client, targetURL, "GET", data, headers)
					if response == nil {
						// Proxy error occurred, continue to the next target
						continue
					}

					fmt.Printf("Thread: Code: %d, URL: %s\n", response.StatusCode, target)

					currentTime := time.Now()
					elapsedTime := currentTime.Sub(lastUpdateTime).Seconds()
					if elapsedTime >= 1.0 {
						rps := float64(requestCount-lastRequestCount) / elapsedTime
						setConsoleTitle(fmt.Sprintf("Request Count: %d | RPS: %.2f", requestCount, rps))
						lastRequestCount = requestCount
						lastUpdateTime = currentTime
					}
				}
			}
		}()
	}

	wg.Wait()

}

func createHTTPClient(proxy string) *http.Client {
	proxyParts := strings.Split(proxy, ":")
	if len(proxyParts) != 4 {
		panic("Invalid proxy format: " + proxy)
	}

	proxyURL := "http://" + proxyParts[2] + ":" + proxyParts[3] + "@" + proxyParts[0] + ":" + proxyParts[1]
	proxyURLParsed, err := url.Parse(proxyURL)
	if err != nil {
		panic(err)
	}

	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxyURLParsed),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:    100,
		IdleConnTimeout: 30 * time.Second,
	}

	return &http.Client{Transport: transport}
}

func getNextTargets(allTargets *[]string, batchSize int) []string {
	targets := []string{}
	for i := 0; i < batchSize && len(*allTargets) > 0; i++ {
		targets = append(targets, (*allTargets)[0])
		*allTargets = (*allTargets)[1:]
	}
	return targets
}

func getRandomProxy(proxies []string) string {
	return proxies[rand.Intn(len(proxies))]
}

func httpRequest(client *http.Client, targetURL string, method string, data []byte, headers map[string]string) *http.Response {
	request, err := http.NewRequest(method, targetURL, bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	for k, v := range headers {
		request.Header.Set(k, v)
	}

	response, err := client.Do(request)
	if err != nil {
		fmt.Printf("Proxy Error: %v\n", err)
		return nil
	}

	body, err := ioutil.ReadAll(response.Body)
	// fmt.Println("Content:", string(body))

	if strings.Contains(string(body), "taken") {
		// Passing
	} else if strings.Contains(string(body), "Available!") { // Username Available

		// Parsing username from url
		rawURL := response.Request.URL.String()
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			fmt.Println("Error:", err)
		}

		queryParams := parsedURL.Query()
		usernameValue := queryParams.Get("username")

		// Sending webhook
		webhookURL := "https://discord.com/api/webhooks/1171822044514623589/VsxoCd9L3GaqbM5f5oT9Fmmk6ajzk1LxWuGbM-wg6iO426BZmhxAJKzOsgb2d1KKquH9"
		message := "Twitter username available.\n```" + usernameValue + "```"

		err1 := webhook.SendDiscordWebhook(webhookURL, message)
		if err1 != nil {
			fmt.Println("Error sending webhook:", err1)
		}

	} else {
		// Passing
	}

	defer response.Body.Close()
	return response
}

func loadProxies(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var proxies []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxies = append(proxies, scanner.Text())
	}

	return proxies, scanner.Err()
}

func loadTargets(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var targets []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		targets = append(targets, scanner.Text())
	}

	return targets, scanner.Err()
}

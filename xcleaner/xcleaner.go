package xcleaner

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

var waitgroup sync.WaitGroup

type Target struct {
	status   int
	duration float64
}

type ServerInfo struct {
	server      string
	server_port string
	password    string
	method      string
	local_port  string
	xserver     string
}

func (serverInfo *ServerInfo) accessGoogleViaProxy() (int, float64) {
	start := time.Now()

	client, _ := socks5Client(fmt.Sprintf("127.0.0.1:%s",
		serverInfo.local_port))
	resp, err := client.Get("http://www.google.com/")

	elapsed := time.Since(start)

	var status int
	var duration float64

	if err == nil {
		status, duration = resp.StatusCode, elapsed.Seconds()
	} else {
		status, duration = 404, elapsed.Seconds()
	}

	return status, duration
}

func (serverInfo *ServerInfo) accessGoogleViaProxyCopy(ch chan Target) (int, float64) {
	start := time.Now()

	client, _ := socks5Client(fmt.Sprintf("127.0.0.1:%s",
		serverInfo.local_port))
	resp, err := client.Get("http://www.google.com/")

	elapsed := time.Since(start)

	var status int
	var duration float64

	if err == nil {
		status, duration = resp.StatusCode, elapsed.Seconds()
	} else {
		status, duration = 404, elapsed.Seconds()
	}

	//for concurrently-run
	ch <- Target{status, duration}

	return status, duration
}

func (serverInfo *ServerInfo) ConcurrentlyAccessGoogleViaProxy(times int) (int, float64) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	ch := make(chan Target, times)

	for i := 0; i < times; i++ {
		go serverInfo.accessGoogleViaProxyCopy(ch)
	}

	var results []Target

	for {
		select {
		case tar := <-ch:
			results = append(results, tar)
			if len(results) == times {
				var duration float64

				for _, tar := range results {
					if tar.status == 404 {
						return 404, tar.duration
					}
					duration += tar.duration
				}
				return 200, duration / float64(len(results))
			}
			//case <-time.After(time.Second * 5):
			//	return 404, 5.0
		default:
		}
		select {
		case <-ticker.C:
			return 404, 5.0
		default:
		}
	}
}

func (serverInfo *ServerInfo) killSSProcess() {
	if runtime.GOOS == "windows" {
		cmdStr := fmt.Sprintf("for /f \"tokens=5\" %a in ('netstat -aon ^| find \"%s\" ^| find \"LISTENING\"') do taskkill /f /pid %a",
			serverInfo.local_port)
		cmd := exec.Command("cmd", "/C", cmdStr)

		if err := cmd.Run(); err != nil {
			panic(err.Error())
		}
	} else {
		cmdStr := fmt.Sprintf("netstat -nlp | grep :%s | awk '{print $7}' | awk -F\"/\" '{ print $1 }'",
			serverInfo.local_port)

		cmd := exec.Command("/bin/sh", "-c", cmdStr)

		if outStr, err := cmd.Output(); err == nil && string(outStr) != "" {
			cmd_str_2 := fmt.Sprintf("kill -9 %s", outStr)
			//fmt.Printf("*    %-40s    *\n", strings.Replace(cmd_str_2, "\n", "", -1))
			cmd2 := exec.Command("/bin/sh", "-c", cmd_str_2)
			if err2 := cmd2.Run(); err2 != nil {
				panic(err2.Error())
			}
		}
	}
}

func New(server string,
	server_port string,
	password string,
	method string,
	local_port string,
	xserver string) *ServerInfo {
	return &ServerInfo{server,
		server_port,
		password,
		method,
		local_port,
		xserver}
}

func (serverInfo *ServerInfo) AvailabilityCheck() {
	waitgroup.Add(1)

	ssLaunchCmd := fmt.Sprintf("sslocal -s %s -p %s -l %s -k \"%s\" -m %s",
		serverInfo.server,
		serverInfo.server_port,
		serverInfo.local_port,
		serverInfo.password,
		serverInfo.method)

	c := make(chan bool)
	go asyncCommand(ssLaunchCmd, c)

	time.Sleep(time.Second * time.Duration(3))
	//time.Sleep(time.Second)

	// 循环访问Google 10次

	//sum_duration := 0.0
	//isDeleted := false
	//
	//for i := 0; i < 10; i++ {
	//	status, duration := serverInfo.accessGoogleViaProxy()
	//	sum_duration += duration
	//
	//	if status != 200 {
	//		delServer(serverInfo.xserver, serverInfo.server)
	//		fmt.Printf("*    cannot access, delete: %-17s    *\n", serverInfo.server)
	//		isDeleted = true
	//		break
	//	}
	//}
	//
	//if !isDeleted {
	//	fmt.Printf("*    %-20s%20.3f    *\n", serverInfo.server, sum_duration/10)
	//	if sum_duration/10 > 2.0 {
	//		delServer(serverInfo.xserver, serverInfo.server)
	//		fmt.Printf("*    high delay, delete: %20s    *\n", serverInfo.server)
	//	}
	//}

	status, duration := serverInfo.ConcurrentlyAccessGoogleViaProxy(10)

	if status != 200 {
		delServer(serverInfo.xserver, serverInfo.server)
		fmt.Printf("*    cannot access, delete: %-17s    *\n", serverInfo.server)
	} else {
		fmt.Printf("*    %-20s%20.3f    *\n", serverInfo.server, duration)
		if duration > 3 {
			delServer(serverInfo.xserver, serverInfo.server)
			fmt.Printf("*    high delay, delete: %20s    *\n", serverInfo.server)
		} else {
			setServerSpeed(serverInfo.xserver, serverInfo.server, duration)
			//fmt.Printf("*    set speed: %4.2f for %20s    *\n", duration, serverInfo.server)
		}
	}

	time.Sleep(time.Millisecond * time.Duration(500))
	<-c

	serverInfo.killSSProcess()

	waitgroup.Done()
}

func (serverInfo *ServerInfo) ToJson() string {
	return fmt.Sprintf("{\"server\": \"%s\", \"server_port\": \"%s\", \"password\": \"%s\",\"method\": \"%s\"}",
		serverInfo.server, serverInfo.server_port, serverInfo.password, serverInfo.method)
}

func asyncCommand(cmdStr string, c chan bool) {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("/bin/sh", "-c", cmdStr)
	}

	if err := cmd.Start(); err != nil {
		fmt.Println(err.Error())
	}

	c <- true

	cmd.Process.Kill()
}

func buildServerInfo(xServerIP string, server string, local_port string) *ServerInfo {
	url := fmt.Sprintf("http://%s/admin?action=get->%s", xServerIP, server)

	resp := HttpGet(url, nil)

	if !strings.HasPrefix(resp, "ERROR") {
		var data []map[string]map[string]string

		json.Unmarshal([]byte(resp), &data)

		serverDict := data[0][fmt.Sprintf("get->%s", server)]

		serverInfo := New(serverDict["server"],
			serverDict["server_port"],
			serverDict["password"],
			serverDict["method"],
			local_port,
			xServerIP)

		return serverInfo
	} else {
		return nil
	}
}

func delServer(xServerIP string, server string) {
	url := fmt.Sprintf("http://%s/admin?action=del->%s", xServerIP, server)

	resp := HttpGet(url, nil)

	if strings.HasPrefix(resp, "ERROR") {
		fmt.Println(resp)
	}
}

func setServerSpeed(xServerIP string, server string, speed float64) {
	url := fmt.Sprintf("http://%s/speed?info=%s$%f", xServerIP, server, speed)

	resp := HttpGet(url, nil)

	if strings.HasPrefix(resp, "ERROR") {
		fmt.Println(resp)
	}
}

func getServers(xServerIP string) []string {
	url := fmt.Sprintf("http://%s/admin?action=getall", xServerIP)
	resp := HttpGet(url, nil)

	if !strings.HasPrefix(resp, "ERROR") {

		var data []map[string]map[string]interface{}

		json.Unmarshal([]byte(resp), &data)

		keys := data[0]["getall"]["keys"]
		servers_il := keys.([]interface{})

		var servers []string

		for _, server := range servers_il {
			serverInfo := strings.Split(server.(string), "--")
			servers = append(servers, serverInfo[0])
		}
		return servers
	} else {
		return nil
	}
}

func putRightTime() {
	now := time.Now()

	rmd := now.Minute() % 10
	seconds := now.Second()

	wait := 60*(10-rmd) - seconds
	if rmd != 0 {
		time.Sleep(time.Second * time.Duration(wait))
	}
}

func HttpGet(url string, headers map[string]string) string {
	req, _ := http.NewRequest("GET", url, nil)

	if headers != nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	//bug fix for request instance invalid address
	req.Close = true

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return "ERROR: " + err.Error()
	}

	defer resp.Body.Close()

	if body, err := ioutil.ReadAll(resp.Body); err == nil {
		return string(body)
	} else {
		return "ERROR: " + err.Error()
	}

}

func socks5Client(addr string, auth ...*proxy.Auth) (client *http.Client, err error) {
	dialer, err := proxy.SOCKS5("tcp", addr,
		nil,
		&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
		},
	)
	if err != nil {
		return
	}

	transport := &http.Transport{
		Proxy:               nil,
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}

	client = &http.Client{
		Transport: transport,
		Timeout:   time.Second * 5,
	}

	return
}

func eachLoop() {
	SingleCheck()

	rmd := time.Now().Minute() % 10
	seconds := time.Now().Second()

	if rmd == 0 {
		time.Sleep(time.Second * time.Duration(62-seconds))
	}
}

func SingleCheck() {
	var sserverGetUrl = "https://github.com/wangleilei2010/xserver/wiki"

	var xserver_ip string

	func() {
		respText := HttpGet(sserverGetUrl, nil)
		reg := regexp.MustCompile(`"markdown\s*\-\s*body">\s*<p>([^<]+)<`)
		for _, matches := range reg.FindAllStringSubmatch(respText, -1) {
			xserver_ip = matches[1]
		}
	}()

	fmt.Printf("*****[%s] check started**********\n", time.Now().Format("2006-01-02 15:04:05"))

	fmt.Printf("*    xserver ip: %-31s *\n", xserver_ip)

	fmt.Println("**************************************************")

	var servers []string
	servers = getServers(xserver_ip)

	if len(servers) < 3 {
		HttpGet(fmt.Sprintf("http://%s/admin?action=flushdb", xserver_ip), nil)

		headers := map[string]string{
			"access-token": "b25seS1mb3ItZmV3LXBlcnNvbnMtdGhhdC1yZWFsbHktbmVlZA",
		}

		HttpGet(fmt.Sprintf("http://%s/servers?computerid=1.1.6-wll17331", xserver_ip),
			headers)
		time.Sleep(time.Second * 3)
		servers = getServers(xserver_ip)
	}

	local_port := 1050

	for idx, server := range servers {
		info := buildServerInfo(xserver_ip,
			server,
			strconv.Itoa(local_port+idx))
		if info != nil {
			go info.AvailabilityCheck()
		}
	}

	waitgroup.Wait()

	fmt.Printf("*****[%s] check finished*********\n\n", time.Now().Format("2006-01-02 15:04:05"))
}

func Loop() {
	for {
		putRightTime()
		eachLoop()
	}
}

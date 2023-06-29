package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/lee-cq/tailscale-transport/logger"
	"tailscale.com/tsnet"
)

type Transport struct {
	RemotePort string `json:"remotePort"`
	LocalPort  string `json:"localPort"`
}

type Config struct {
	Hostname  string `json:"hostname"`
	Authkey   string `json:"authkey"`
	Ephemeral bool   `json:"ephemeral"`
	Dir       string `json:"dir"`

	Transports []Transport `json:"transports"`
}

var (
	config     = Config{}
	configFile = flag.String("c", "config.json", "配置文件")
	newFile    = flag.Bool("n", false, "在当前目录中创建模板")
)

func createConfig() {
	dir, _ := os.Getwd()
	config := Config{
		Hostname:  "注册在TSNET中的主机名",
		Authkey:   "认证密钥",
		Ephemeral: false,
		Dir:       "文件存储位置",

		Transports: []Transport{
			{
				RemotePort: "Remote:Port",
				LocalPort:  "Local:port",
			},
			{
				RemotePort: "New",
				LocalPort:  "New",
			},
		},
	}

	jsonBytes, _ := json.Marshal(config)

	fmt.Printf("在 %s 目录下创建 config.json .... \n", dir)
	err := os.WriteFile("config.json", jsonBytes, 0o0664)
	if err != nil {
		fmt.Println("Write Error")
	}
	fmt.Println(" Done.")
}

func GetConfig() {
	flag.Parse()

	if *newFile {
		createConfig()
		os.Exit(0)
	}

	jsonBytes, err := os.ReadFile(*configFile)
	if err != nil {
		fmt.Printf("无效的配置文件或路径: %v \n", err)
		os.Exit(1)
	}

	err = json.Unmarshal(jsonBytes, &config)
	if err != nil {
		fmt.Printf("Json 解析失败 %v \n", err)
		os.Exit(1)
	}
	logger.Info("配置解析完成, hostname: %s", config.Hostname)
}

func main() {
	defer fmt.Println("All End ...")
	GetConfig()

	logger.Info("初始化 tsnet.Server, 配置相关服务")
	tsServer := new(tsnet.Server)
	tsServer.Hostname = config.Hostname
	tsServer.Ephemeral = config.Ephemeral
	if config.Authkey != "" {
		tsServer.AuthKey = config.Authkey
	}
	if config.Dir != "" {
		tsServer.Dir = config.Dir
	}
	defer fmt.Println("tsnet Server a has topped .")
	defer func(tsServer *tsnet.Server) {
		err := tsServer.Close()
		if err != nil {
			fmt.Println("Tsnet Close Error.")
			return
		}
	}(tsServer)

	fmt.Println("开始链接至tsnet")
	if err := tsServer.Start(); err != nil {
		return
	}
	ip4, _ := tsServer.TailscaleIPs()
	logger.Info("tsnet Start Success, IP: %s", ip4)
	// 配置tsServer完成 ...

	mainSignal := make(chan int)
	for _, transport := range config.Transports {
		fmt.Printf("创建Goroutine %s -> %s\n", transport.RemotePort, transport.LocalPort)
		go func(transport Transport) {
			local2RemoteTCP(tsServer, transport.RemotePort, transport.LocalPort)
			mainSignal <- 1
		}(transport)
	}

	defer fmt.Println("Ok, Will be Done ... ")
	var allDone int
	allT := len(config.Transports)
	for {
		allDone += <-mainSignal
		if allDone >= allT {
			break
		}
	}
}

func local2RemoteTCP(tsServer *tsnet.Server, remote string, local string) {

	defer logger.Info("已经关闭  %s <--> %s", local, remote)
	listener, err := net.Listen("tcp", local)
	if err != nil {
		logger.Warn("Listen on %s error: %s", local, err.Error())
		return
	}
	defer listener.Close()

	for {
		logger.Info("Wait for connection on %s", local)

		localConn, err := listener.Accept()
		if err != nil {
			logger.Warn("Handle local connect error: %s", err.Error())
			continue
		}

		go func() { // 在gorouine中开启线程
			defer localConn.Close()

			logger.Info("Connection from %s", localConn.RemoteAddr().String())
			logger.Info("Connecting " + remote)

			// ctx := context.W
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer func() {
				cancel()
			}()

			remoteConn, err := tsServer.Dial(ctx, "tcp", remote)
			if err != nil {
				logger.Warn("Connect remote %s error: %s", remote, err.Error())
				return
			}
			defer func() {
				err := remoteConn.Close()
				if err != nil {
					logger.Warn("remoteConn Close Error...")
				}
			}()

			logger.Info("Open pipe: %s <== FWD ==> %s",
				localConn.RemoteAddr().String(), remoteConn.RemoteAddr().String())

			PipeForward(localConn, remoteConn)

			logger.Info("Close pipe: %s <== FWD ==> %s",
				localConn.RemoteAddr().String(), remoteConn.RemoteAddr().String())

		}()

	}
}

func PipeForward(connA net.Conn, connB net.Conn) {
	// 创建管道
	pipeSignal := make(chan struct{}, 1)

	go func() {
		Copy(connA, connB)
		pipeSignal <- struct{}{}
	}()

	go func() {
		Copy(connB, connA)
		pipeSignal <- struct{}{}
	}()

	<-pipeSignal
}

func Copy(dst net.Conn, src net.Conn) (int, error) {
	buffer := make([]byte, 0x8000)
	var written int
	var err error

	for {
		var nr int
		var er error

		nr, er = src.Read(buffer)
		if nr > 0 {
			var nw int
			var ew error

			nw, ew = dst.Write(buffer[:nr])

			if nw > 0 {
				logger.Info("%s == [%d bytes] ==> %s", src.RemoteAddr().String(), nw, dst.LocalAddr().String())
				written += nw
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

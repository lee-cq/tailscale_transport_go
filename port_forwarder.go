package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
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

var config Config
var (
	configFile = flag.String("c", "config.json", "配置文件")
	newFile    = flag.Bool("n", false, "在当前目录中创建模板")
)

var logger = log.WithFields(log.Fields{"name": "port-forwarder"})

// 创建配置文件模版
func createConfig() {

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

	dir, _ := os.Getwd()
	logger.Info("在 %s 目录下创建 config.json .... \n", dir)
	err := os.WriteFile("config.json", jsonBytes, 0o0664)
	if err != nil {
		logger.Error("Write Error")
	}
	logger.Info(" Done.")
}

// GetConfig 命令行参数解析
func GetConfig() {
	flag.Parse()

	if *newFile {
		createConfig()
		os.Exit(0)
	}

	jsonBytes, err := os.ReadFile(*configFile)
	if err != nil {
		logger.Error("无效的配置文件或路径: %v \n", err)
		os.Exit(1)
	}

	err = json.Unmarshal(jsonBytes, &config)
	if err != nil {
		logger.Error("Json 解析失败 %v \n", err)
		os.Exit(1)
	}
	log.WithFields(log.Fields{}).Info("配置解析完成, hostname: %s", config.Hostname)
}

func main() {
	defer logger.Info("All End ...")
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
	defer logger.Info("tsnet Server a has topped .")
	defer func(tsServer *tsnet.Server) {
		err := tsServer.Close()
		if err != nil {
			logger.Error("Tsnet Close Error.")
			return
		}
	}(tsServer)

	logger.Info("开始链接至tsnet")
	if err := tsServer.Start(); err != nil {
		return
	}
	ip4, _ := tsServer.TailscaleIPs()
	logger.Info("tsnet Start Success, IP: %s", ip4)
	// 配置tsServer完成 ...

	mainSignal := make(chan int)
	for i := 0; i < len(config.Transports); i++ {
		logger.Info("创建Goroutine %s -> %s\n", config.Transports[i].RemotePort, config.Transports[i].LocalPort)
		go func(transport Transport) {
			local2RemoteTCP(tsServer, transport.RemotePort, transport.LocalPort)
			mainSignal <- 1
		}(config.Transports[i])
	}

	defer logger.Info("Ok, Will be Done ... ")
	var allDone int
	allT := len(config.Transports)
	for {
		allDone += <-mainSignal
		if allDone >= allT {
			logger.Warn("All Goroutines is Done. Will Break.")
			break
		}
	}
}

func local2RemoteTCP(tsServer *tsnet.Server, remote string, local string) {

	defer logger.Info("已经关闭  %s <--> %s", local, remote)
	// 创建 监听端口
	listener, err := net.Listen("tcp", local)
	if err != nil {
		logger.Warn("Listen on %s error: %s", local, err.Error())
		return
	}
	defer func(listener net.Listener) {
		_ = listener.Close()
	}(listener)

	for {
		logger.Info("Wait for connection on %s", local)
		// 等待连接接入
		localConn, err := listener.Accept()
		if err != nil {
			logger.Warn("Handle local connect error: %s", err.Error())
			continue
		}

		go func() { // 在goroutines中开启线程
			defer func(localConn net.Conn) {
				_ = localConn.Close()
			}(localConn)

			logger.Info("Connection from %s", localConn.RemoteAddr().String())
			logger.Info("Connecting " + remote)

			// 创建上下文
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer func() {
				cancel()
			}()

			// 创建远程连接并处理错
			remoteConn, err := tsServer.Dial(ctx, "tcp", remote)
			if err != nil {
				logger.Warn("Connect remote %s error: %s", remote, err.Error())
				return
			}
			// 结束时关闭远程连接，并处理错误
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

func PipeForward(connA net.Conn, connB net.Conn) int {
	// 创建管道
	pipeSignal := make(chan int)

	go func() {
		sizeA, _ := Copy(connA, connB)
		pipeSignal <- sizeA
	}()

	go func() {
		sizeB, _ := Copy(connB, connA)
		pipeSignal <- sizeB
	}()
	var totalSize int
	totalSize += <-pipeSignal
	return totalSize
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
				logger.Debug("%s == [%d bytes] ==> %s", src.RemoteAddr().String(), nw, dst.LocalAddr().String())
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

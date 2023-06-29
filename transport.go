// 端口转发
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

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
	}
	t := Transport{
		RemotePort: "RemoteIP:port",
		LocalPort:  "LocalIP:port",
	}
	config.Transports = append(config.Transports, t)

	jsonBytes, _ := json.Marshal(config)

	println("在 %s 目录下", dir)
	err := os.WriteFile("config.json", jsonBytes, 'w')
	if err != nil {
		fmt.Println("Write Error")
	}
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
}

func main() {
	// 初始化配置

	defer fmt.Println("All End ...")

	GetConfig()

	fmt.Println("初始化 tsnet.Server, 配置相关服务")
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
	// 配置tsServer完成 ...

	fmt.Println("开始链接至tsnet")
	if err := tsServer.Start(); err != nil {
		return
	}
	// tsServer完成
	for _, transport := range config.Transports {
		go Transporter(tsServer, transport.RemotePort, transport.LocalPort)
	}
	defer fmt.Println("Ok, Will be Done ... ")

	for {
		time.Sleep(time.Minute)
	}
}

func Transporter(tsServer *tsnet.Server, remotePort string, localPort string) {
	fmt.Println("监听端口", localPort)
	listener, err := net.Listen("tcp", localPort)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	for {
		// 收到信号退出

		connLocal, err := listener.Accept()
		fmt.Printf("收到连接: %-v", connLocal)
		if err != nil {
			fmt.Println("Error: ", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		tsConn, err := tsServer.Dial(ctx, "tcp", remotePort)
		if err != nil {
			cancel()
			fmt.Println("Error: ", err)
			continue
		}

		go handleConnection(connLocal, tsConn)
	}
}

func handleConnection(connLocal net.Conn, connRemote net.Conn) {
	var rxByte int64 = 0
	var txByte int64 = 0
	var hasError = true

	// 结束时关闭连接
	defer func(tsConn net.Conn) {
		_ = tsConn.Close()
	}(connLocal)

	defer func(tsConn net.Conn) {
		_ = tsConn.Close()
	}(connRemote)

	// 开启 Local -> Remote 数据拷贝 goroutine
	go func() {
		_, err := copyData(connLocal, connRemote, &rxByte)
		if err != nil {
			fmt.Printf("conn Error: %v\n", err)
			hasError = true
			return
		}
	}()

	// 开启 Remote -> Local 数据拷贝 goroutine
	go func() {
		_, err := copyData(connRemote, connLocal, &txByte)
		if err != nil {
			fmt.Printf("remote Error: %v\n", err)
			hasError = true
			return
		}
	}()

	// 更新进度到控制台
	for {
		time.Sleep(time.Second)
		fmt.Printf("\rrx: %d, tx: %d", rxByte, txByte)
		if hasError {
			fmt.Println("")
			return
		}
	}
}

/** 数据拷贝goroutine
 * 在一个循环中监听并
 */
func copyData(src net.Conn, dst net.Conn, pLen *int64) (int, error) {
	for {

		buf := make([]byte, 256)

		n, err := src.Read(buf)
		if err != nil {
			return 0, err
		}

		n, err = dst.Write(buf[:n])
		if err != nil {
			return 0, err
		}
		*pLen += int64(n)
	}
	// return *p_len, _
}

// 端口转发
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"time"

	"tailscale.com/tsnet"
)

var (
	remotePort = flag.String("remotePort", "nil", "tailscale中的远程端口, 格式: <ip|Hostname>:port")
	localPort  = flag.String("localPort", "nil", "本地需要被监听的端口，格式 [ip|hostname]:port")
	hostname   = flag.String("hostname", "tsnet-translate", "hostname to use on the tsnet")
)

func main() {

	flag.Parse()

	fmt.Println("初始化 tsnet.Server")
	tsServer := new(tsnet.Server)
	tsServer.Hostname = *hostname

	defer fmt.Println("tsnet Server a has topped .")
	defer func(tsServer *tsnet.Server) {
		err := tsServer.Close()
		if err != nil {
			return
		}
	}(tsServer)

	fmt.Println("开始链接至tsnet")
	if err := tsServer.Start(); err != nil {
		return
	}
	fmt.Println("监听端口 8888")
	listener, err := net.Listen("tcp", *localPort)
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

		tsConn, err := tsServer.Dial(ctx, "tcp", *remotePort)
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

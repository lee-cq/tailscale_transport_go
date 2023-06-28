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
	remote_port = flag.String("remote_port", "nil", "tailscale中的远程端口, 格式: <ip|Hostname>:port")
	local_port  = flag.String("local_port", "nil", "本地需要被监听的端口，格式 [ip|hostname]:port")
	hostname    = flag.String("hostname", "tsnet-translate", "hostname to use on the tailnet")
)

func main() {

	flag.Parse()

	fmt.Println("初始化 tsnet.Server")
	ts_server := new(tsnet.Server)
	ts_server.Hostname = *hostname

	defer fmt.Println("tsnet Server stoped ...")
	defer ts_server.Close()

	fmt.Println("开始链接至tsnet")
	if err := ts_server.Start(); err != nil {
		return
	}
	fmt.Println("监听端口 8888")
	listener, err := net.Listen("tcp", *local_port)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	for {
		// 收到信号退出

		conn_local, err := listener.Accept()
		fmt.Printf("收到连接: %-v", conn_local)
		if err != nil {
			fmt.Println("Error: ", err)
			continue
		}
		defer conn_local.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		ts_conn, err := ts_server.Dial(ctx, "tcp", *remote_port)
		if err != nil {
			fmt.Println("Error: ", err)
			continue
		}
		defer ts_conn.Close()

		go handleConnection(conn_local, ts_conn)
	}
}

func handleConnection(conn_local net.Conn, conn_remote net.Conn) {
	var rx_byte int64 = 0
	var tx_byte int64 = 0
	var has_error bool = true

	// remote, err := net.Dial("tcp", "localhost:9999")
	// if err != nil {
	// 	fmt.Println("Error: ", err)
	// 	return
	// }

	go func() {
		_, err := copyData(conn_local, conn_remote, &rx_byte)
		if err != nil {
			fmt.Printf("conn Error: %v\n", err)
			has_error = true
			return
		}
	}()

	go func() {
		_, err := copyData(conn_remote, conn_local, &tx_byte)
		if err != nil {
			fmt.Printf("remote Error: %v\n", err)
			return
		}
	}()

	for {
		time.Sleep(time.Second)
		fmt.Printf("\rrx: %d, tx: %d", rx_byte, tx_byte)
		if has_error {
			fmt.Println("")
			return
		}
	}
}

/** 数据拷贝goroute
 * 在一个循环中监听并
 */
func copyData(src net.Conn, dst net.Conn, p_len *int64) (int, error) {
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
		*p_len += int64(n)
	}
	// return *p_len, _
}

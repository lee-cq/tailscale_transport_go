# tailscale_transport_go
使用TailScale实现本地端口转发。

使用环境变量提供 AuthKey
windows : `set TS_AUTHKEY=xxx`      
Linux & MacOS : `export TS_AUTHKEY=xxx`   

安装依赖和构建   
```bash
go get
go build -o dist/ -v ./...
```

all:node console

node:
	go build -v -o node ./raft.go

console:
	go build -v -o console ./raft_console.go

# 加上这一句才会每次都更新node console
.PHONY: node console

.PHONY: test server agent-windows

test:
	go test ./...

server:
	go run ./apps/server

agent-windows:
	mkdir -p bin
	GOOS=windows GOARCH=amd64 go build -o bin/remote-admin-agent.exe ./apps/agent-windows

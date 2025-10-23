NAME=cfst
BINDIR=bin
GOBUILD=CGO_ENABLED=0 go build -ldflags '-X main.version=v1.0.0 -w -s -buildid='
#GOBUILD=CGO_ENABLED=0 /Users/kian/opt/go/bin/go build -ldflags '-w -s -buildid='
# The -w and -s flags reduce binary sizes by excluding unnecessary symbols and debug info
# The -buildid= flag makes builds reproducible

all: linux-amd64 linux-arm64 macos-amd64 macos-arm64 win64 win32

linux-amd64:
	rm -f bin/cfst && GOARCH=amd64 GOOS=linux $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ main.go && upx bin/cfst-linux-amd64 && mv bin/cfst-linux-amd64 bin/cfst

macos-arm64:
	GOARCH=arm64 GOOS=darwin $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ main.go


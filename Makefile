# Makefile for go-test-parser cross-compilation

MAIN_PACKAGE_PATH=./cmd/testparser
BINARY_NAME=go-test-parser
BUILD_DIR=./build

.PHONY: all clean windows-amd64 linux-amd64 darwin-arm64

all: windows-amd64 linux-amd64 darwin-arm64

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PACKAGE_PATH)

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PACKAGE_PATH)

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PACKAGE_PATH)

clean:
	rm $(BUILD_DIR)/*

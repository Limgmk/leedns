# Go parameters
GOCMD = go
BUILD = $(GOCMD) build
CLEAN = $(GOCMD) clean
BINARY_NAME = leedns
ARCH = amd64
TARGET_DIR = target

build:
	@$(BUILD) -o $(TARGET_DIR)/$(BINARY_NAME) -v

run: build
	@./$(TARGET_DIR)/$(BINARY_NAME) $(args)

clean:
	@$(CLEAN)
	@rm -rf $(TARGET_DIR)

build-linux:
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) $(BUILD) -ldflags '-s -w --extldflags "-static -fpic"' -o $(TARGET_DIR)/$(BINARY_NAME)-linux-$(ARCH) -v

build-docker: build-linux
	@docker build -t leedns:latest .
	@docker image prune -f

INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: build install

build:
	go build -o jira2gh

install:
	install -d "$(INSTALL_DIR)"
	go build -o "$(INSTALL_DIR)/jira2gh"

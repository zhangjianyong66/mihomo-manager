.PHONY: install uninstall help

PREFIX ?= $(HOME)/.local

help:
	@echo "Mihomo Manager - Mihomo 代理管理工具"
	@echo ""
	@echo "使用方法:"
	@echo "  make install    安装到 $(PREFIX)/bin"
	@echo "  make uninstall  卸载"
	@echo ""
	@echo "或直接运行:"
	@echo "  ./scripts/install.sh    安装"
	@echo "  ./scripts/uninstall.sh  卸载"

install:
	@./scripts/install.sh

uninstall:
	@./scripts/uninstall.sh
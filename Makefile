.PHONY: install uninstall test-install help

help:
	@echo "Mihomo Manager - Mihomo 代理管理工具"
	@echo "支持 Ubuntu/Debian 与 macOS 12+（amd64/arm64）"
	@echo ""
	@echo "使用方法:"
	@echo "  make install       安装依赖、mihomo core、mm 和用户 daemon（默认跳过 CN 规则集下载）"
	@echo "  make install WITH_RULESETS=1  同时下载并安装 CN 规则集"
	@echo "  make uninstall     卸载 mm，默认保留 core 和配置"
	@echo "  make test-install  运行隔离安装测试"
	@echo ""
	@echo "或直接运行:"
	@echo "  ./scripts/install.sh    安装"
	@echo "  ./scripts/uninstall.sh  卸载"

install:
	@if [ "$(WITH_RULESETS)" = "1" ]; then ./scripts/install.sh --with-rulesets; else ./scripts/install.sh; fi

uninstall:
	@./scripts/uninstall.sh

test-install:
	@./scripts/tests/test_install.sh

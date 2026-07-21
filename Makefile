.PHONY: install uninstall test-install help

help:
	@echo "Mihomo Manager - Mihomo 代理管理工具"
	@echo ""
	@echo "使用方法:"
	@echo "  make install       安装依赖、规则集、mihomo core、mm 和用户 daemon"
	@echo "  make uninstall     卸载 mm，默认保留 core 和配置"
	@echo "  make test-install  运行隔离安装测试"
	@echo ""
	@echo "或直接运行:"
	@echo "  ./scripts/install.sh    安装"
	@echo "  ./scripts/uninstall.sh  卸载"

install:
	@./scripts/install.sh

uninstall:
	@./scripts/uninstall.sh

test-install:
	@./scripts/tests/test_install.sh

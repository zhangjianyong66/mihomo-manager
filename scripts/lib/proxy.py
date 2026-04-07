#!/usr/bin/env python3
"""
mihomo-manager 节点解析模块
负责解析各种格式的订阅节点（vless, vmess, trojan, ss）
"""

import sys
import re
import base64
import urllib.parse
import argparse
import json


def parse_vless(url: str) -> dict:
    """解析 vless:// 链接"""
    # vless://uuid@host:port?params#name
    match = re.match(r'vless://([^@]+)@([^:]+):(\d+)\?(.*)#(.*)', url)
    if not match:
        raise ValueError(f"无效的 vless URL: {url[:50]}...")
    
    uuid, host, port, params, name = match.groups()
    name = urllib.parse.unquote(name or '未命名节点')
    
    proxy = {
        'name': name,
        'type': 'vless',
        'server': host,
        'port': int(port or 443),
        'uuid': uuid,
        'cipher': 'auto',
        'udp': True
    }
    
    # 解析参数
    param_dict = {}
    for p in params.split('&'):
        if '=' in p:
            k, v = p.split('=', 1)
            param_dict[k] = urllib.parse.unquote(v)
    
    # 网络类型
    if param_dict.get('type') == 'tcp':
        proxy['network'] = 'tcp'
    elif param_dict.get('type') == 'ws':
        proxy['network'] = 'ws'
        proxy['ws-opts'] = {
            'path': param_dict.get('path', '/'),
            'headers': {'Host': param_dict.get('host', host)}
        }
    elif param_dict.get('type') == 'grpc':
        proxy['network'] = 'grpc'
        proxy['grpc-opts'] = {
            'grpc-service-name': param_dict.get('serviceName', param_dict.get('path', ''))
        }
    
    # 安全设置
    if param_dict.get('security') == 'reality':
        proxy['tls'] = True
        proxy['servername'] = param_dict.get('sni', host)
        proxy['reality-opts'] = {
            'public-key': param_dict.get('pbk', ''),
            'short-id': param_dict.get('sid', '')
        }
        proxy['client-fingerprint'] = param_dict.get('fp', 'chrome')
    elif param_dict.get('security') == 'tls':
        proxy['tls'] = True
        proxy['servername'] = param_dict.get('sni', host)
    
    if param_dict.get('flow'):
        proxy['flow'] = param_dict['flow']
    
    return proxy


def parse_vmess(url: str) -> dict:
    """解析 vmess:// 链接"""
    # vmess://base64_encoded_json
    encoded = url.replace('vmess://', '')
    
    try:
        decoded_json = base64.b64decode(encoded).decode('utf-8')
        config = json.loads(decoded_json)
    except Exception as e:
        raise ValueError(f"无效的 vmess URL: {e}")
    
    proxy = {
        'name': config.get('ps', '未命名节点'),
        'type': 'vmess',
        'server': config.get('add', ''),
        'port': int(config.get('port', 443)),
        'uuid': config.get('id', ''),
        'cipher': config.get('scy', 'auto'),
        'alterId': int(config.get('aid', 0)),
        'udp': True
    }
    
    if config.get('tls') == 'tls':
        proxy['tls'] = True
        proxy['servername'] = config.get('host', config.get('add', ''))
    
    if config.get('net') == 'ws':
        proxy['network'] = 'ws'
        proxy['ws-opts'] = {
            'path': config.get('path', '/'),
            'headers': {'Host': config.get('host', config.get('add', ''))}
        }
    
    return proxy


def parse_trojan(url: str) -> dict:
    """解析 trojan:// 链接"""
    # trojan://password@host:port?params#name
    match = re.match(r'trojan://([^@]+)@([^:]+):(\d+)\?(.*)#(.*)', url)
    if not match:
        raise ValueError(f"无效的 trojan URL: {url[:50]}...")
    
    password, host, port, params, name = match.groups()
    name = urllib.parse.unquote(name or '未命名节点')
    
    proxy = {
        'name': name,
        'type': 'trojan',
        'server': host,
        'port': int(port or 443),
        'password': password,
        'udp': True,
        'skip-cert-verify': False
    }
    
    return proxy


def parse_ss(url: str) -> dict:
    """解析 ss:// 链接"""
    # ss://base64(method:password)@host:port#name
    match = re.match(r'ss://([^@]+)@([^:]+):(\d+)#(.*)', url)
    if not match:
        raise ValueError(f"无效的 ss URL: {url[:50]}...")
    
    encoded, host, port, name = match.groups()
    
    try:
        decoded = base64.b64decode(encoded).decode('utf-8')
        method, password = decoded.split(':', 1)
    except Exception as e:
        raise ValueError(f"解析 ss 认证信息失败: {e}")
    
    name = urllib.parse.unquote(name or '未命名节点')
    
    proxy = {
        'name': name,
        'type': 'ss',
        'server': host,
        'port': int(port),
        'cipher': method,
        'password': password,
        'udp': True
    }
    
    return proxy


def parse_subscription(content: str) -> list:
    """
    解析订阅内容，返回节点列表
    支持 base64 编码和纯文本格式
    """
    lines = []
    
    # 尝试 base64 解码
    try:
        decoded = base64.b64decode(content.strip()).decode('utf-8')
        lines = decoded.strip().split('\n')
    except:
        # 不是 base64，直接使用原始内容
        lines = content.strip().split('\n')
    
    proxies = []
    errors = []
    
    for line in lines:
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        
        try:
            if line.startswith('vless://'):
                proxies.append(parse_vless(line))
            elif line.startswith('vmess://'):
                proxies.append(parse_vmess(line))
            elif line.startswith('trojan://'):
                proxies.append(parse_trojan(line))
            elif line.startswith('ss://'):
                proxies.append(parse_ss(line))
            else:
                # 忽略不支持的格式
                pass
        except Exception as e:
            errors.append(str(e))
    
    return proxies, errors


def merge_proxies(proxies: list, config_file: str) -> dict:
    """
    将解析的节点合并到现有配置
    返回更新后的配置字典
    """
    import yaml
    
    # 读取现有配置
    with open(config_file, 'r') as f:
        config = yaml.safe_load(f) or {}
    
    if not config:
        raise ValueError("无法读取现有配置")
    
    # 提取特殊节点（DIRECT, REJECT 等）
    special_nodes = ['DIRECT', 'REJECT']
    existing_proxies = config.get('proxies', [])
    proxy_names = [p['name'] for p in proxies]
    
    # 保留特殊节点
    for p in existing_proxies:
        name = p.get('name', '')
        if name.startswith('官网') or name.startswith('有效期'):
            proxies.insert(0, p)
            proxy_names.insert(0, name)
        if name in special_nodes:
            proxies.insert(0, p)
            proxy_names.insert(0, name)
    
    # 更新 proxies
    config['proxies'] = proxies
    
    # 更新 proxy-groups 中的节点列表
    for group in config.get('proxy-groups', []):
        if group.get('type') == 'select' and group.get('name') not in ['🎯 Direct', '🛑 Block', '🐟 漏网之鱼']:
            # 保留特殊节点
            new_proxies = [p for p in group.get('proxies', []) if p in special_nodes]
            # 添加新节点
            new_proxies.extend(proxy_names)
            group['proxies'] = new_proxies
    
    return config


def main():
    parser = argparse.ArgumentParser(description='解析 Mihomo 订阅节点')
    parser.add_argument('input', help='订阅文件路径或 - 表示从 stdin 读取')
    parser.add_argument('-c', '--config', help='现有配置文件路径（用于合并）')
    parser.add_argument('-o', '--output', help='输出文件路径')
    parser.add_argument('-j', '--json', action='store_true', help='以 JSON 格式输出')
    parser.add_argument('-v', '--verbose', action='store_true', help='显示详细信息')
    
    args = parser.parse_args()
    
    # 读取订阅内容
    if args.input == '-':
        content = sys.stdin.read()
    else:
        try:
            with open(args.input, 'r') as f:
                content = f.read()
        except FileNotFoundError:
            print(f"错误: 文件不存在: {args.input}", file=sys.stderr)
            sys.exit(1)
        except Exception as e:
            print(f"错误: 无法读取文件: {e}", file=sys.stderr)
            sys.exit(1)
    
    # 解析节点
    proxies, errors = parse_subscription(content)
    
    if args.verbose:
        print(f"解析到 {len(proxies)} 个节点", file=sys.stderr)
        if errors:
            print(f"解析错误 ({len(errors)} 个):", file=sys.stderr)
            for e in errors[:5]:
                print(f"  - {e}", file=sys.stderr)
    
    if not proxies:
        print("错误: 没有解析到有效节点", file=sys.stderr)
        sys.exit(1)
    
    # 合并配置或输出节点列表
    if args.config:
        try:
            result = merge_proxies(proxies, args.config)
            if args.json:
                output = json.dumps(result, ensure_ascii=False, indent=2)
            else:
                import yaml
                output = yaml.dump(result, allow_unicode=True, default_flow_style=False, sort_keys=False)
        except Exception as e:
            print(f"错误: 合并配置失败: {e}", file=sys.stderr)
            sys.exit(1)
    else:
        if args.json:
            output = json.dumps(proxies, ensure_ascii=False, indent=2)
        else:
            import yaml
            output = yaml.dump(proxies, allow_unicode=True, default_flow_style=False, sort_keys=False)
    
    # 输出结果
    if args.output:
        with open(args.output, 'w') as f:
            f.write(output)
        print(f"已保存到: {args.output}")
    else:
        print(output)


if __name__ == '__main__':
    main()

use serde::Serialize;
use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::time::Duration;
use tokio::net::UdpSocket;
use tokio::time::timeout;

// ==================== STUN (RFC 8489) 客户端 ====================
// 仅用于打洞候选端点收集：向 STUN 服务器发 Binding Request，解析 XOR-MAPPED-ADDRESS，
// 得到 NAT 后的公网反射地址。不做 NAT 类型判断（在真实互联网上不可靠）。

const STUN_BINDING_REQUEST: u16 = 0x0001;
const STUN_MAGIC_COOKIE: u32 = 0x2112_A442;
const ATTR_XOR_MAPPED_ADDRESS: u16 = 0x0020;
const ATTR_HEADER_LEN: usize = 20;

/// 候选端点：打洞时对端会尝试向这些地址发包。
#[derive(Debug, Clone, Serialize)]
pub struct Endpoint {
    pub addr: String,
    #[serde(rename = "type")]
    pub typ: String,
}

fn build_binding_request(transaction_id: &[u8; 12]) -> Vec<u8> {
    let mut msg = Vec::with_capacity(ATTR_HEADER_LEN);
    msg.extend_from_slice(&STUN_BINDING_REQUEST.to_be_bytes());
    msg.extend_from_slice(&0u16.to_be_bytes()); // length = 0 (无属性)
    msg.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
    msg.extend_from_slice(transaction_id);
    msg
}

/// 解析 XOR-MAPPED-ADDRESS 属性。成功返回 (type, SocketAddr)。
fn parse_xor_mapped(msg: &[u8], transaction_id: &[u8; 12]) -> Option<(u16, SocketAddr)> {
    if msg.len() < ATTR_HEADER_LEN + 4 {
        return None;
    }
    let _msg_type = u16::from_be_bytes([msg[0], msg[1]]);
    let attr_len = u16::from_be_bytes([msg[2], msg[3]]) as usize;
    let cookie = u32::from_be_bytes([msg[4], msg[5], msg[6], msg[7]]);
    if cookie != STUN_MAGIC_COOKIE || msg.len() < ATTR_HEADER_LEN + attr_len {
        return None;
    }
    if msg[8..20] != transaction_id[..] {
        return None;
    }

    let mut off = ATTR_HEADER_LEN;
    let end = ATTR_HEADER_LEN + attr_len;
    while off + 4 <= end {
        let atype = u16::from_be_bytes([msg[off], msg[off + 1]]);
        let alen = u16::from_be_bytes([msg[off + 2], msg[off + 3]]) as usize;
        let vstart = off + 4;
        if vstart + alen > end {
            return None;
        }
        if atype == ATTR_XOR_MAPPED_ADDRESS && alen >= 8 {
            // value: [1B res][1B family][2B X-Port][4B X-Address]
            let family = msg[vstart + 1];
            if family == 0x01 {
                let x_port = u16::from_be_bytes([msg[vstart + 2], msg[vstart + 3]]);
                let port = x_port ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
                let x_ip = u32::from_be_bytes([
                    msg[vstart + 4],
                    msg[vstart + 5],
                    msg[vstart + 6],
                    msg[vstart + 7],
                ]);
                let ipv4 = Ipv4Addr::from(x_ip ^ STUN_MAGIC_COOKIE);
                return Some((atype, SocketAddr::new(IpAddr::V4(ipv4), port)));
            }
        }
        off = vstart + alen + pad4(alen);
    }
    None
}

fn pad4(n: usize) -> usize {
    (4 - (n % 4)) % 4
}

/// 向单个 STUN 服务器做 Binding，返回反射地址。
pub async fn stun_binding(server: &str, timeout_ms: u64) -> Result<SocketAddr, String> {
    let server_addr: SocketAddr = match server.parse() {
        Ok(addr) => addr,
        Err(_) => tokio::net::lookup_host(server)
            .await
            .map_err(|e| format!("resolve STUN server addr: {server}: {e}"))?
            .next()
            .ok_or_else(|| format!("STUN server resolved to no address: {server}"))?,
    };
    let sock = UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0))
        .await
        .map_err(|e| format!("bind failed: {e}"))?;
    let transaction_id = rand_id();
    let req = build_binding_request(&transaction_id);

    let fut = async {
        sock.send_to(&req, server_addr).await.map_err(|e| e.to_string())?;
        let mut buf = [0u8; 512];
        loop {
            let (n, src) = sock.recv_from(&mut buf).await.map_err(|e| e.to_string())?;
            if src != server_addr {
                continue;
            }
            if let Some((_t, addr)) = parse_xor_mapped(&buf[..n], &transaction_id) {
                return Ok(addr);
            }
        }
    };
    let result = timeout(Duration::from_millis(timeout_ms), fut).await;
    match result {
        Ok(Ok(addr)) => Ok(addr),
        Ok(Err(e)) => Err(e),
        Err(_) => Err(format!("STUN {server} timed out")),
    }
}

/// 生成随机 transaction id。
fn rand_id() -> [u8; 12] {
    let mut id = [0u8; 12];
    // 无 rand 直接依赖时用系统时间 + 栈熵；打洞场景无需密码学强度。
    let t = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let bytes = t.to_le_bytes();
    for (i, b) in bytes.iter().enumerate() {
        id[i % 12] ^= *b;
    }
    id
}

/// 枚举本机 IPv4 接口地址，作为候选端点的「本机地址」来源。
/// 过滤逻辑与 collector.rs 的 should_count_network_interface 一致（排除虚拟/容器/隧道网卡）。
pub fn local_ipv4_candidates() -> Vec<Ipv4Addr> {
    let mut out = Vec::new();
    match if_addrs::get_if_addrs() {
        Ok(ifaces) => {
            for iface in ifaces {
                if !should_count_interface(&iface.name) {
                    continue;
                }
                if let IpAddr::V4(ip) = iface.ip() {
                    if !ip.is_loopback() && !ip.is_unspecified() {
                        out.push(ip);
                    }
                }
            }
        }
        Err(_) => {}
    }
    out
}

fn should_count_interface(name: &str) -> bool {
    let normalized = name.trim().to_ascii_lowercase();
    if normalized.is_empty() || normalized == "lo" {
        return false;
    }
    const VIRTUAL_PREFIXES: &[&str] = &[
        "docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "zt", "tailscale", "tun", "tap",
        "wg", "flannel", "cni", "kube", "calico", "cali", "podman", "nerdctl", "containerd",
        "ifb", "ip6tnl", "sit", "gre", "gretap", "erspan", "dummy",
    ];
    if VIRTUAL_PREFIXES.iter().any(|p| normalized.starts_with(p)) {
        return false;
    }
    const VIRTUAL_MARKERS: &[&str] = &[
        "vethernet", "hyper-v", "virtualbox", "vmware", "wsl", "docker", "tailscale", "zerotier",
        "npcap", "tap-windows", "wireguard",
    ];
    !VIRTUAL_MARKERS.iter().any(|m| normalized.contains(m))
}

/// 收集候选端点：本机 IPv4 + 各 STUN 服务器反射地址。
/// `egress_ip` 为可选的已知出口公网 IP（来自 collector.rs 的 HTTP egress 探测），仅作端口未知的地址提示。
pub async fn collect_endpoints(
    stun_servers: &[String],
    egress_ip: Option<&str>,
    timeout_ms: u64,
) -> Vec<Endpoint> {
    let mut endpoints = Vec::new();
    for ip in local_ipv4_candidates() {
        endpoints.push(Endpoint {
            addr: ip.to_string(),
            typ: "local".to_string(),
        });
    }
    for server in stun_servers {
        match stun_binding(server, timeout_ms).await {
            Ok(addr) => endpoints.push(Endpoint {
                addr: addr.to_string(),
                typ: "stun".to_string(),
            }),
            Err(_) => continue,
        }
    }
    if let Some(ip) = egress_ip {
        if !endpoints.iter().any(|e| e.addr.starts_with(&format!("{ip}:"))) {
            endpoints.push(Endpoint {
                addr: ip.to_string(),
                typ: "egress".to_string(),
            });
        }
    }
    endpoints
}

#[cfg(test)]
mod tests {
    use super::*;

    // mock STUN 服务器：在环回 UDP 上监听，收到 Binding Request 即回 XOR-MAPPED-ADDRESS。
    async fn spawn_mock_stun() -> SocketAddr {
        let sock = UdpSocket::bind((Ipv4Addr::LOCALHOST, 0)).await.unwrap();
        let addr = sock.local_addr().unwrap();
        tokio::spawn(async move {
            let mut buf = [0u8; 512];
            loop {
                match sock.recv_from(&mut buf).await {
                    Ok((n, src)) => {
                        if n < ATTR_HEADER_LEN {
                            continue;
                        }
                        let tid: [u8; 12] = buf[8..20].try_into().unwrap();
                        let mut resp = Vec::new();
                        resp.extend_from_slice(&0x0101u16.to_be_bytes()); // Binding Response
                        resp.extend_from_slice(&12u16.to_be_bytes()); // 属性区总长(attr header 4 + XOR-MAPPED 8)
                        resp.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
                        resp.extend_from_slice(&tid);
                        resp.extend_from_slice(&ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
                        resp.extend_from_slice(&8u16.to_be_bytes());
                        resp.push(0); // reserved
                        resp.push(0x01); // IPv4
                        let port = src.port() ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
                        resp.extend_from_slice(&port.to_be_bytes());
                        let ip = match src.ip() {
                            IpAddr::V4(ip) => u32::from(ip),
                            IpAddr::V6(_) => u32::from(Ipv4Addr::LOCALHOST),
                        };
                        resp.extend_from_slice(&(ip ^ STUN_MAGIC_COOKIE).to_be_bytes());
                        let _ = sock.send_to(&resp, src).await;
                    }
                    Err(_) => break,
                }
            }
        });
        addr
    }

    #[tokio::test]
    async fn binding_gets_reflexive_address() {
        let addr = spawn_mock_stun().await;
        let res = stun_binding(&addr.to_string(), 2000).await.expect("stun binding failed");
        assert_eq!(res.ip(), IpAddr::V4(Ipv4Addr::LOCALHOST));
        assert!(res.port() > 0);
    }

    #[tokio::test]
    async fn binding_timeout_on_unreachable() {
        let res = stun_binding("127.0.0.1:1", 300).await;
        assert!(res.is_err());
    }

    #[tokio::test]
    async fn parse_xor_mapped_roundtrip() {
        let src: SocketAddr = "192.168.1.5:4000".parse().unwrap();
        let tid = [9u8; 12];
        let mut resp = Vec::new();
        resp.extend_from_slice(&0x0101u16.to_be_bytes());
        resp.extend_from_slice(&12u16.to_be_bytes());
        resp.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
        resp.extend_from_slice(&tid);
        resp.extend_from_slice(&ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
        resp.extend_from_slice(&8u16.to_be_bytes());
        resp.push(0);
        resp.push(0x01);
        let port = src.port() ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
        resp.extend_from_slice(&port.to_be_bytes());
        let ip = match src.ip() {
            IpAddr::V4(ip) => u32::from(ip),
            IpAddr::V6(_) => unreachable!(),
        };
        resp.extend_from_slice(&(ip ^ STUN_MAGIC_COOKIE).to_be_bytes());

        let parsed = parse_xor_mapped(&resp, &tid).expect("parse failed");
        assert_eq!(parsed.1, src);
    }

    #[tokio::test]
    async fn collect_endpoints_includes_local_and_stun() {
        let addr = spawn_mock_stun().await;
        let eps = collect_endpoints(&[addr.to_string()], Some("203.0.113.9"), 2000).await;
        assert!(!eps.is_empty());
        assert!(eps.iter().any(|e| e.typ == "stun"));
        assert!(eps.iter().any(|e| e.typ == "egress"));
    }
}

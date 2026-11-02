# DeepSeek PoW 纯算实现

当前服务端 PoW 已走纯 Go 实现：`internal/deepseek/pow.go` 负责从上游 challenge map 中取字段，调用 `ds2api/pow` 求解 nonce，并组装 `x-ds-pow-response` header。

## 算法

DeepSeekHashV1 = SHA3-256 但 **Keccak-f[1600] 跳过 round 0** (只做 rounds 1..23)。其余参数不变:
rate=136, padding=0x06+0x80, output=32 字节。

PoW 协议:服务端选 answer ∈ [0, difficulty),计算 `challenge = hash(prefix + str(answer))`。
客户端遍历 [0, difficulty) 找到匹配的 nonce。

```
prefix = salt + "_" + str(expire_at) + "_"
input  = (prefix + str(nonce)).encode("utf-8")
hash   = DeepSeekHashV1(input)      → 32 bytes
header = base64(json({algorithm, challenge, salt, answer, signature, target_path}))
```

## 主要入口

- `pow/deepseek_hash.go`：DeepSeekHashV1 / Keccak-f[1600] rounds 1..23。
- `pow/deepseek_pow.go`：`SolvePow`、`BuildPowHeader`、`SolveAndBuildHeader`。
- `internal/deepseek/pow.go`：服务侧适配层，校验 `algorithm == DeepSeekHashV1` 并调用 `pow.SolvePow`。

## 测试

```bash
cd pow && go test -v ./... && go test -bench=. -benchmem
```

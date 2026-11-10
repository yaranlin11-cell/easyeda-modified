# 保留实例时的显式连接修改

人工构造的三脚器件回归，不含真实工程资料或库器件。`pins.json` 覆盖 NC→连接、
旧网→新网、未连接→NC。测试在内存构造完整原始回读和 Compose 源后填入精确字节 SHA256；
文件本身缺少哈希，不能直接生成写入队列。坐标沿用合成 Compose 测试的 raw 单位。

运行 `go test ./internal/app -run 'TestConnectivityEdits|TestComposePaperlessPreserved'`。
断言缺项、重复、错旧值、未知/遗漏字段、哈希过期、器件与引脚名变化均写前拒绝；
队列只删除明确图形，保留器件身份，显式取消 NC 并先回读再接线。此处只验证离线编译与
模拟回读，不代表现场执行、保存重载或硬件电气验证。

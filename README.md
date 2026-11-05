# kos-node

KOS 平台项目：每节点代理 — 采集本节点设备事实并写入名字等于自身主机名的 NodeDevice。

- 设计：`tsic/kos-design` 仓的 `kos-node/`（范围、契约、验收、决策记录）。
- 每节点一个实例；只写自身主机名的 NodeDevice，不写其他对象的 spec、status 或标记。

## 布局

| 路径 | 内容 |
|---|---|
| `cmd/kos-node/` | 进程入口：主机名即采集身份与写入对象名，按间隔执行采集轮次 |
| `internal/collector/` | 设备事实的写入：边界拒绝、同序列号去重与线格式；`SysfsSource` 读本机块设备 |
| `internal/agent/` | 采集循环：一轮失败只记录，下一轮照常采集 |
| `tests/acceptance/` | 验收用例，目录名等于设计仓用例编号 |

## 边界

- 只写名字等于自身主机名的 NodeDevice，`spec.node` 等于对象名（NODE-C-37）。
- 越界写入由 kosd 的校验器标记无效；陈旧与回收、设备视图由 kosd 承担，本项目不实现第二套。
- 四层事实各自独立上报：硬件存在、Linux 已识别、驱动就绪由本机 sysfs 证明；发布为可调度资源由发布该设备的系统写入，本阶段没有发布者，采集器不置真。
- 占用按内核的持有关系判定：设备被阵列或设备映射器持有时标为占用，来源是持有它的子系统，空闲条目不携带该结构。

## 构建与测试

```bash
make verify      # gofmt + go vet
make test        # 单元测试 + 验收测试（自动下载 envtest 二进制）
make acceptance  # 只跑 tests/acceptance，输出用例名
go build ./...   # 构建节点代理
```

验收测试使用 envtest 启动真实 kube-apiserver 与 etcd，装的是 kosd 仓的对象定义；
环境变量 `KUBEBUILDER_ASSETS` 指向 envtest 二进制目录，`make test` 会下载并设置。

## 运行

```bash
kos-node --kubeconfig ~/.kube/config --interval 1m
```

`--kubeconfig` 为空时使用 Pod 内的服务账号配置；`--once` 只跑一轮，`--sysfs` 指定 sysfs 挂载点。

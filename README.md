# kbmatch

客服机器人知识库匹配引擎（纯标准库，进程内存存储）。

## 用法

```go
eng := kbmatch.New()
errs := eng.Import([]kbmatch.Entry{...}) // 每批最多 1000 条，按 id 新增或整条替换；任一行不合法则整批不生效
eng.Delete([]string{"id1", "id2"})       // 整批删除，未知 id 忽略
res, found, err := eng.Match("用户消息")  // 返回得分最高且达标的条目
```

- `Entry.Threshold`：最多两位小数的十进制字符串，取值 0.00–1.00。
- `Term`：1–20 个词条，权重 1–100；词条切分后为 1–8 个词元，条内去重。
- 达标线：`hitWeight/totalWeight >= max(0.6, threshold)`，整数精确判定，无浮点误差；同分取 id 字节序最小者。
- `Match` 可在多 goroutine 并发调用，并与 `Import`/`Delete` 并发；写操作之间需调用方串行。

## 测试

```sh
go test ./...        # 单元 + 差分 + 性能耗时比测试
go test -short ./... # 跳过性能耗时比测试
go test -race ./...  # 竞态检测
```

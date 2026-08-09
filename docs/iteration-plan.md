# LLM Gateway 项目 —— 可迭代工程计划

## 整体设计思路

Gateway 是整个 AI 四层架构里唯一直接面对 LLM Provider 故障、限频、超时的组件。它解决的不是 AI 问题,是分布式系统的经典可靠性问题——只是被代理的对象换成了 LLM API,而且这个"被代理的对象"有一个普通API网关不用处理的特性:**响应是流式的、时间跨度可以长达几分钟、且和"钱"直接挂钩(按token计费)**。这三个特性决定了这份计划不能照搬普通网关的设计顺序。

迭代顺序调整为:**先跑通 → 先统一协议 → 先啃流式这块硬骨头 → 再谈可靠性 → 再谈可观测 → 再谈安全和成本 → 最后诚实面对单实例的局限**。

每一次迭代都遵循同一个模板:

1. 这次迭代解决的是哪个具体问题
2. 技术选型 + 为什么选这个,不选别的
3. 怎么验收——用什么指标/行为证明这次迭代确实生效了
4. 产出物

---

## Iteration 0:最简反向代理——先让请求能转发

**目的**

跑通最基础的反向代理:请求进来 → 转发到 LLM Provider → 响应返回。验证 Go 标准库做代理的可行性,不涉及任何路由/限流逻辑。

**具体做什么**

- 用 Go 标准库 `net/http/httputil.ReverseProxy` 实现最简代理,不引入任何第三方Web框架
- 硬编码一个目标地址(如 `http://localhost:11434`,Ollama 本地服务)
- 配置文件用最简单的 `config.yaml`,只定义一个 `target_url`
- 启动后用 curl 发请求验证能拿到 Ollama 的回复

**为什么这么选**

`httputil.ReverseProxy` 已经是生产级实现,原生支持流式响应转发(这一点很关键,为Iteration 2铺路)。先硬编码再配置化,是为了先验证网络连通性这些基础问题,不被过早的抽象掩盖。

**验收标准**

```bash
curl -X POST http://localhost:8080/api/generate \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3","prompt":"你好"}'
```

能返回 Ollama 的生成结果,Gateway 日志里能看到请求转发记录。

**过渡说明(为什么这里用Ollama原生格式,不是提前用OpenAI格式)**

这一步刻意用Ollama的原生格式(`/api/generate` + `prompt`字段),而不是提前用Iteration 1才会统一的OpenAI格式(`/v1/chat/completions` + `messages`字段)。原因是:如果这一步直接对Gateway发OpenAI格式请求,较新版本的Ollama自己也支持这个兼容端点,请求很可能"意外地"能跑通——但这时候验证到的是"Ollama认识这个格式",不是"Gateway做对了什么"。用Ollama原生格式能明确证明这一步Gateway没做任何Schema转换,纯粹是管道转发。如果现在对Gateway发OpenAI格式请求,大概率会被原样转发给Ollama、大概率不被正确处理——这正是Iteration 1要解决的问题。

**产出物**:能跑的最简反向代理 + `config.yaml` 配置文件骨架

---

## Iteration 1:协议统一 + 多Provider路由

**目的**

解决两个问题:一是"不同模型在不同后端,调用方不想记多个URL和多套请求格式";二是这一步是后续所有可靠性机制(重试、熔断)的底座——先有多个可切换的目标,才能谈切换和降级。

**这一步的核心原则:Schema转换是唯一路径,不留旁路**

一个真正做到"一个入口通所有模型"的Gateway,对外应该固定暴露一种统一格式(建议对齐OpenAI的 `/v1/chat/completions` 格式,因为这是事实上的行业标准,大部分SDK和工具默认支持这个格式),内部按 `model` 字段路由到不同Provider时,做请求体和响应体的双向转换。

这里刻意不设计passthrough(跳过转换、原样透传)这条旁路,即便真实企业网关里这是常见功能。原因是:现在没有任何一个具体场景需要用到某个Provider的专有参数,为一个假设性需求先搭好配置项、解析逻辑、测试用例,是过早通用化——和这份计划里"不上Redis直到真需要、不上重框架直到真需要"是同一类判断。等哪天真的接入一个有专有能力、统一Schema确实覆盖不了的Provider,再回来加这条路径,那时候需求是具体的,设计也会更准。

**技术选型**

| 组件 | 选择 | 为什么 |
|------|------|--------|
| 对外统一格式 | OpenAI Chat Completions 格式 | 事实标准,上层RAG/Agent不用关心底层是Claude还是DeepSeek还是本地Ollama |
| Schema转换层 | 每个Provider适配器实现统一接口,含非流式的 `ToProviderRequest()` / `FromProviderResponse()`,以及流式的 `TransformStreamChunk()` | 新增一个Provider只需要实现接口方法,不改路由核心逻辑;流式方法在这一步先占位,避免Iteration 2引入流式时要回头改接口签名 |
| 路由依据 | 统一请求体里的 `model` 字段 | 调用方无感知,改配置就能切模型 |
| 路由配置 | `config.yaml` 里的 `routes` 列表,含 `model_pattern`(支持通配符)、`target`、`api_key_env`、`adapter`(指定用哪个Schema转换适配器) | 纯文本配置,改路由不换代码 |

**具体做什么**

- 实现OpenAI格式作为Gateway对外的统一Schema
- 定义统一的Adapter接口,流式方法在这一步先占位,不实现:

```go
type ProviderAdapter interface {
    // 非流式,本迭代实现
    ToProviderRequest(unifiedReq *ChatCompletionRequest) ([]byte, error)
    FromProviderResponse(providerResp []byte) (*ChatCompletionResponse, error)

    // 流式,本迭代先占位,Iteration 2 补实现
    TransformStreamChunk(providerChunk []byte) (*ChatCompletionChunk, error)
}

// 本迭代阶段的占位实现
func (a *OllamaAdapter) TransformStreamChunk(chunk []byte) (*ChatCompletionChunk, error) {
    return nil, errors.New("not implemented: see Iteration 2")
}
```

- 为Ollama、DeepSeek各写一个适配器,做请求/响应的双向格式转换(字段名映射、参数默认值补齐、错误码归一化)
- API Key 通过 `api_key_env` 从环境变量读取,配置文件里不留明文
- 路由匹配失败时返回清晰的JSON错误,不是网关自身500堆栈

**为什么在这一步就定义流式接口**

如果Iteration 1的Adapter接口只声明非流式两个方法,Iteration 2引入流式转发时就要回头改接口签名,已经写好的两个适配器都要跟着改。提前占位、Iteration 2只补实现不改签名,是标准的"接口先行"做法,没有额外成本。

**验收标准**

- 用统一的OpenAI格式请求体,分别传 `model: "llama3"` 和 `model: "deepseek-chat"`,Gateway能正确转换成各自Provider的原生格式发出去,拿到响应后转换回统一格式返回
- 对比同一个问题在两个Provider下,调用方拿到的响应结构完全一致(字段名、层级),只有内容不同
- 配置一个不存在的model,返回明确错误而不是网关崩溃

**产出物**:统一Schema定义 + 至少2个Provider的适配器 + 路由配置文档

---

## Iteration 2:流式响应——LLM Gateway最难啃的一块骨头

**目的**

LLM API的主流调用方式是流式(SSE),如果这一步不提前做,后面的重试、限流、观测全部要返工。这个迭代专门解决三件事:流式转发本身、客户端断连的传播、超时的分层设计。这是整个Gateway计划里最容易被忽略、但优先级必须提到可靠性机制之前的一环。

**具体做什么**

**1. 流式转发**

- `httputil.ReverseProxy` 原生支持流式转发,但需要确认 `FlushInterval` 设置正确(设为负数表示每次写入立即flush,不缓冲),否则SSE的实时性会被Go默认的缓冲策略破坏
- 统一Schema层(Iteration 1)在流式场景下需要对每个chunk做增量格式转换,不能等流结束了再统一转换——这意味着适配器需要额外实现一个 `TransformStreamChunk()` 方法,和非流式的 `FromProviderResponse()` 分开

**2. 客户端断连传播**

- 用户关闭连接/取消请求时,Gateway必须把这个取消信号透传到上游请求,而不是自己继续等上游生成完
- 实现方式:用 `http.Request.Context()`,当客户端断连时该context会被取消,Gateway发往上游的请求复用同一个context,上游请求会因为context取消而自动终止
- 这一点对企业场景是硬要求——不这么做的话,用户取消了请求,Provider那边还在计费生成,纯浪费

**3. 超时分层**

流式场景下不能用一个笼统的"请求超时",需要拆成三层:

| 超时类型 | 含义 | 建议默认值 |
|---|---|---|
| 连接超时(Dial Timeout) | 和上游建立TCP连接的超时 | 5s |
| 首字节超时(TTFB) | 从发出请求到收到第一个chunk的最大等待时间 | 30s |
| chunk间空闲超时(Idle Timeout) | 流式过程中,两个chunk之间的最大间隔——超过说明上游卡住了,不是正常生成慢 | 60s |

`http.Server` 默认的 `ReadTimeout`/`WriteTimeout` 如果直接套用,会在长流式响应场景下把正常连接掐断,必须显式关闭或调大这两个默认超时,改用上面三层自定义超时逻辑。

**为什么这么选**

这三件事(转发、断连传播、分层超时)缺一不可,而且必须在讨论"重试要不要做"之前先解决——因为重试策略在流式场景下的可行性,完全取决于这三件事有没有做对。

**验收标准**

- 用一个会流式输出较长内容的模型请求,验证Gateway转发的chunk和上游产生的chunk在时序上基本一致(没有被Go缓冲攒批)
- 手动在流式请求进行到一半时,客户端主动断开连接(比如curl加 `--max-time` 提前掐断),观察上游Provider的请求是否也被同步取消(可以在Ollama日志或者用一个mock上游服务器打印日志验证)
- 模拟上游"连上了但迟迟不返回第一个chunk"的场景(比如指向一个只accept不response的端口),验证TTFB超时生效并返回明确错误,而不是无限挂起
- 模拟上游"流开始了但中途卡住不再发chunk"的场景,验证idle timeout生效

**产出物**:支持流式转发的Gateway + 断连传播实现 + 分层超时配置 + 三个故障场景的测试脚本

---

## Iteration 3:限流(Token维度) + 重试 + 熔断

**目的**

解决"某个Provider挂了/触发限频,导致所有请求失败"的问题。限流算法选token bucket,但维度按LLM真实的Token特性设计,而不是照搬普通API网关常见的"按请求数"限流;重试策略要考虑Iteration 2引入的流式场景。

**技术选型**

| 组件 | 选择 | 为什么 |
|------|------|--------|
| 限流算法 | Token Bucket,但维护两个独立的桶:请求数桶(RPM)和token数桶(TPM) | LLM Provider的真实限流是RPM+TPM双维度,一个请求可能带10万token的上下文,只按请求数限流完全反映不了真实的配额压力 |
| Token预估 | 请求发出前,用简单的字符数/4估算prompt token数(不追求精确,只求量级对),先按预估值扣减TPM配额;拿到Provider真实返回的usage字段后,再做配额修正(多退少补) | 精确计算token需要引入对应模型的tokenizer,学习项目没必要为了限流精度引入这个依赖,估算+事后修正是工程上更常见的折中方案 |
| 实现方式 | `golang.org/x/time/rate`,每个Provider维护两个rate.Limiter实例 | 成熟稳定,不需要从零实现令牌桶算法 |
| 重试策略 | 区分**非流式**和**流式**两种场景分别处理 | 见下方详细说明 |
| 熔断策略 | 简单计数器:连续失败5次→熔断30秒→半开状态试探→成功关闭/失败重新计时 | 逻辑透明,容易调试和讲清楚 |

**重试策略的流式/非流式区分**

- **非流式请求**:响应还没开始返回给调用方,可以在Gateway内部透明重试。区分可重试错误(超时、502/503/504)和不可重试错误(400/401/402),后者直接返回不重试
- **流式请求**:一旦第一个chunk已经吐给调用方,Gateway就**不能**做透明重试了——已经发出去的内容没法撤回。这种情况下,Gateway唯一能做的是:如果在TTFB超时内(还没吐出第一个chunk)发生错误,可以透明重试;一旦开始流式输出后发生错误,只能把错误作为一个特殊的流式事件(比如SSE的 `event: error`)发给调用方,由调用方自己决定要不要重新发起整个请求

**熔断后的降级——必须对调用方透明,不能是静默的**

"熔断后自动路由到降级Provider,上层调用方无感知"这种设计是一个正确性风险——降级模型(比如本地Ollama)和原Provider(比如GPT-4级别模型)的输出质量差异巨大,调用方如果完全不知道这次是降级结果,可能会把打折的答案当正常结果处理(尤其是RAG模块会把生成结果当成最终答案交给用户)。

这一版改为:降级发生时,响应头里加 `X-Gateway-Fallback: true` 和 `X-Gateway-Fallback-Reason: circuit_open`,调用方可以选择读取这个头、决定要不要接受降级结果,或者在展示给用户时加一个提示。

**事前声明:allow_fallback配置**

`X-Gateway-Fallback` 响应头是事后通知——请求已经发生了,调用方只能在拿到结果之后判断要不要接受。但有些场景(比如生成对外展示的正式内容)根本不允许用降级模型凑合,调用方宁愿直接拿到错误、走自己的异常处理逻辑,也不想承担"事后发现是降级结果"的风险。这种场景需要一个事前声明的开关,和事后通知配合起来才完整:

```yaml
routes:
  - name: deepseek
    model_pattern: "deepseek*"
    fallback_provider: ollama-local
    allow_fallback: true   # 默认true;调用方在意结果质量时可在请求级别覆盖为false
```

当 `allow_fallback: false` 时,熔断触发后Gateway不路由到降级Provider,直接返回503,由调用方自己决定重试或走异常处理,不会被塞一个质量不确定的降级结果。

**具体做什么**

- 每个Provider配置独立的RPM和TPM限额
- 实现请求前的token预估函数,以及拿到真实usage后的配额修正逻辑
- 流式/非流式分别实现对应的重试逻辑
- 熔断降级时,在响应头里加透明标记

**验收标准**

- 配置一个较低的TPM(比如1000),连续发几个token量较大的请求,验证在还没达到RPM上限的情况下,TPM先触发429
- 手动模拟Provider宕机,验证连续失败5次后熔断,后续请求路由到降级Provider,响应头里能看到 `X-Gateway-Fallback: true`
- 把某条路由配置为 `allow_fallback: false`,重复上面的宕机模拟,验证熔断后Gateway直接返回503,不路由到降级Provider
- 流式请求场景下,模拟"已经吐出几个chunk后上游报错"的情况,验证Gateway不会做透明重试,而是发送一个错误事件给调用方
- 非流式场景下,模拟同样的错误,验证Gateway能透明重试且调用方无感知(拿不到错误,只是延迟增加)
- 对400错误(不可重试),验证直接返回不重试

**产出物**:Token维度限流实现 + 流式感知的重试逻辑 + 带透明标记的熔断降级 + 故障模拟测试脚本

---

## Iteration 4:可观测性——知道出问题了,更知道问题在哪

**目的**

Iteration 2、3上线后,可能出问题的地方变多了(TTFB超时?idle超时?限流?熔断?降级Provider也挂了?),这时候需要日志、指标、健康检查提供排查依据。这一版额外补充了流式场景特有的观测指标。

**技术选型**

| 组件 | 选择 | 为什么 |
|------|------|--------|
| 日志 | `log/slog`(Go 1.21+标准库结构化日志) | 零依赖,原生JSON输出 |
| 指标暴露 | `promhttp`(Prometheus Go客户端) | 业界标准 |
| 请求追踪 | `X-Request-ID`,调用方传入则复用,没传入则自动生成UUID | 串联Gateway和Provider两端日志,不需要上OpenTelemetry全家桶 |
| 健康检查 | `/health` 返回200,后续可扩展为检查所有Provider连通性的深度健康检查 | 最简实现起步 |

**具体做什么**

- 每次请求结束后记录一行结构化日志:`request_id`、`model`、`provider`、`latency_ms`、`status_code`、`tokens_used`(prompt/completion分开记)、`retry_count`、`is_fallback`、`error`
- 流式请求额外记录:`ttfb_ms`(首字节延迟)、`stream_duration_ms`(整个流的持续时间)、`chunk_count`
- 暴露Prometheus指标:
  - `gateway_requests_total`(按provider、status分类)
  - `gateway_request_duration_seconds`(P50/P95/P99延迟直方图,流式和非流式分开统计,否则流式请求的长耗时会污染非流式的延迟分布)
  - `gateway_ttfb_seconds`(流式请求首字节延迟直方图,这是流式场景用户体感最直接的指标)
  - `gateway_retries_total`
  - `gateway_circuit_breaker_state`
  - `gateway_tokens_total`(按provider、caller分类,为Iteration 5的成本控制做数据基础)
- 暴露 `/health` 端点

**为什么把流式指标单独列出来**

如果流式和非流式的延迟混在一个指标里统计,P95/P99会完全失真——一个流式请求可能持续几十秒,一个非流式短请求可能几百毫秒,混在一起看不出任何有意义的分布。TTFB是流式场景下真正决定用户体感的指标,必须单独统计。

**验收标准**

- Prometheus查询能看到按provider分类的请求量曲线
- 流式和非流式的延迟指标在Grafana上是两条独立的曲线
- 模拟一次超时错误,能通过`request_id`串起Gateway入口和Provider调用的完整链路日志
- 模拟熔断,`gateway_circuit_breaker_state`能看到完整的状态变化过程
- `/health`正常返回200

**产出物**:结构化日志 + Prometheus指标(含流式专属指标) + 健康检查 + Grafana看板(至少包含请求量、延迟(流式/非流式分开)、TTFB、熔断状态四张图)

---

## Iteration 5:安全加固 + 成本预算控制

**目的**

解决三件事:API Key暴露风险、Gateway自身访问控制、以及**调用方级别的成本失控风险**。把成本控制和安全放在同一个迭代,因为本质上都是"防止失控调用"。

**技术选型**

| 组件 | 选择 | 为什么 |
|------|------|--------|
| API Key管理 | 环境变量 + `os.Getenv`(Iteration 1已实现) | 敏感信息不落配置文件 |
| 调用方鉴权 | 静态Bearer Token(配置 `gateway.auth.token`,支持每个调用方一个独立Token) | 学习项目不需要OAuth/OIDC |
| 鉴权开关 | `gateway.auth.enabled`,默认false(本地调试友好),生产环境true | 减少本地调试摩擦 |
| 成本预算 | 每个调用方(按Token区分身份)配置 `daily_token_budget`,基于Iteration 4已经在记录的`gateway_tokens_total`做累计判断 | 内部中台里,Agent模块的多步骤循环一旦出bug陷入死循环,没有预算兜底会持续消耗token直到有人发现 |
| 预算超限行为 | 超过预算后,返回429并明确说明是预算超限(区别于限流的429),而不是直接拒绝所有后续请求到次日——给一个"紧急提额"的配置项供人工临时调整 | 全自动拒绝可能会在业务真正需要的时候误伤,保留人工干预空间 |

**具体做什么**

- 确认所有Provider API Key都通过环境变量读取,配置文件里搜不到明文
- 给Gateway自身加Bearer Token鉴权中间件
- 实现每个调用方的每日token用量累计计数器(可以复用Iteration 4已经在记录的usage数据,做一个按天聚合的滚动窗口)
- 超过预算阈值时返回明确的429 + 错误信息说明是预算超限,而非普通限流
- 提供一个管理接口或配置热更新方式,允许人工临时调整某个调用方的预算上限

**验收标准**

- 不带Authorization header的请求返回401
- 带错误Token返回403
- `git grep` 在配置文件目录下搜不到任何API Key明文
- 手动把某个调用方的每日预算设置得很低,验证达到预算后请求被拒绝,错误信息明确说明是预算超限而不是普通限流
- 验证预算拒绝和限流429返回的错误信息可以被区分(比如加一个 `error_type` 字段)

**产出物**:安全加固版Gateway + 调用方级别成本预算控制 + 部署文档

---

## Iteration 6:诚实面对单实例局限——多实例扩展路径(设计说明,非必须实现)

**目的**

Iteration 3的限流/熔断状态、Iteration 5的成本预算计数器,目前都是Go进程内存里的状态。这在单实例部署下没问题,但如果Gateway要做高可用(多副本部署),当前设计会导致每个实例各算各的账,完全不同步。如果不把这个局限提前写清楚,很容易让人误以为现在的设计天然支持多实例部署。这一迭代不要求真的搭一套多实例环境,但要求把这个局限写清楚,并给出明确的升级路径,和RAG计划里对内存级限流的坦诚处理方式保持一致。

**具体做什么**

- 在项目文档里明确写一条"已知限制":当前限流器、熔断器状态、成本预算计数器均为单实例内存态,多实例部署下各实例状态不同步,会导致实际生效的限流/预算阈值是配置值乘以实例数
- 给出升级路径设计(不要求实现):
  - 限流状态迁移到Redis,用 `INCR` + `EXPIRE` 实现分布式滑动窗口计数,替代本地的 `rate.Limiter`
  - 熔断器状态迁移到Redis或etcd,多实例共享同一个熔断状态,避免"实例A已经熔断,实例B还在傻乎乎地重试"这种不一致
  - 成本预算计数器同样需要迁移到Redis,用原子自增操作保证多实例下的计数准确
- 如果学习目的需要动手验证,可以用Docker Compose起两个Gateway实例+一个Redis,做一次最小可用的分布式限流demo,验证思路是否work,不需要做到生产级完整实现

**为什么这么选**

诚实标注已知限制,是工程判断力的一部分——很多学习项目的问题不是"做得不够复杂",而是"复杂度评估不诚实",让别人(或者未来的自己)误以为一个单实例demo已经具备了生产级的可扩展性。这一条本身不需要复杂实现,但必须写清楚。

**验收标准**

- 文档里有明确的"已知限制"章节
- 升级路径的设计说明完整,能回答"如果现在要上生产、要做多实例,接下来第一步该改哪里"这个问题
- (可选)如果做了Docker Compose的两实例demo,验证Redis共享状态下,两个实例对同一个调用方的限流计数是同步的

**产出物**:已知限制说明文档 + 多实例升级路径设计 + (可选)最小可用的Redis共享状态demo

---

## 技术栈总览

```
语言:          Go 1.21+(标准库 + x/time/rate + promhttp)
配置:          config.yaml(路由规则、Schema适配器、限流参数、熔断阈值、鉴权开关、预算配置)
Schema统一:     自定义OpenAI格式适配器层(每Provider一个双向转换实现)
流式处理:       httputil.ReverseProxy(FlushInterval调优) + context传播 + 三层超时
限流:          x/time/rate,双维度(RPM+TPM),token预估+事后修正
日志:          slog(结构化JSON输出)
指标:          Prometheus(promhttp暴露/metrics),流式/非流式指标分离
成本控制:       按调用方的每日token预算计数器
部署:          单二进制文件(go build产物),Docker可选
多实例路径:      Redis共享状态(设计已给出,实现可选)
测试工具:       curl + 自定义故障模拟脚本(改hosts/iptables模拟宕机,mock慢响应上游测超时)
```

---

## 迭代顺序背后的逻辑

- **Iteration 0(最简代理)** 跑通后,发现只有一个Provider、格式也是Provider原生的,调用方要自己适配不同格式 → **Iteration 1(协议统一+路由)** 把"改配置切模型"和"统一对外格式"一起解决
- **Iteration 1** 上线后,你会发现流式请求在当前实现下要么被Go默认缓冲策略拖慢实时性,要么客户端取消了请求上游还在傻跑 → **Iteration 2(流式响应)** 专门啃这块硬骨头,而且必须在讨论重试之前解决,因为重试策略在流式场景下的可行性完全取决于这一步
- **Iteration 2** 上线后,你发现某个Provider挂了或者流式请求中途报错,需要决定重试/熔断策略,而且必须区分流式和非流式两种截然不同的处理方式 → **Iteration 3(限流+重试+熔断)** 补上可靠性,并且限流维度按LLM真实的Token特性设计,不是照搬普通API网关的RPS
- **Iteration 3** 上线后,可能出问题的地方变多了(TTFB超时?限流?熔断?降级?) → **Iteration 4(可观测性)** 给排查提供数据支撑,流式和非流式指标分开看
- **Iteration 4** 上线后,你发现配置文件里还有API Key明文、任何人能访问Gateway、而且Agent模块的多步骤循环一旦失控会持续烧token没有兜底 → **Iteration 5(安全加固+成本预算)** 堵上这两类失控风险
- 最后,**Iteration 6** 不是新功能,是诚实盘点——前面所有的限流/熔断/预算状态都是单实例内存态,如果要往生产走、要做高可用,这是必须面对且提前写清楚的下一步,而不是留到真出问题了才发现。

这个顺序里最关键的一处设计是:**流式响应被放在了可靠性机制之前**,因为它不是一个"锦上添花"的功能迭代,而是决定后续每一层设计对不对的前提条件。
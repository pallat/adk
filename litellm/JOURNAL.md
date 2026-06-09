# Development Journal: litellm adapter สำหรับ ADK Go

> บันทึกการพัฒนา `github.com/pallat/adk/litellm` — custom `model.LLM` adapter
> ที่ทำให้ ADK Go ใช้ model ผ่าน LiteLLM proxy (หรือ OpenAI-compatible endpoint ใดก็ได้)

---

## ที่มา

ADK Go v1.4.0 มี built-in adapter แค่ 2 ตัว: `model/gemini` กับ `model/apigee`  
ทั้งคู่ผูกกับ Google Cloud — ไม่มีทางใช้ OpenAI, Ollama, Claude, หรือ model local ได้เลย

เป้าหมายของ course นี้คือสอนโดยไม่ lock-in vendor → จึงต้องสร้าง adapter เอง

**LiteLLM** คือ proxy ที่เปิด OpenAI-compatible API แล้ว route ไปหา model ต่างๆ ได้ กลายเป็น "universal adapter" ตัวกลาง

---

## v0: โครงสร้างพื้นฐาน

### สิ่งที่รู้ก่อนเริ่ม

ADK กำหนด interface นี้ให้ implement:

```go
type LLM interface {
    Name() string
    GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error]
}
```

`LLMRequest` มี field หลัก:
- `Contents []*genai.Content` — conversation history
- `Config *genai.GenerateContentConfig` — system instruction, tools, etc.
- `Tools map[string]any` — tool instances (by name)

`LLMResponse` ต้องการ:
- `Content *genai.Content` — model's response
- `TurnComplete bool` — บอก ADK ว่า turn นี้จบหรือยัง

### v0 implementation

แปลง `Contents` → OpenAI messages format แล้ว POST `/chat/completions`  
แปลง response กลับเป็น `*genai.Content`

```
genai.Content{Role: "user", Parts: [{Text: "สวัสดี"}]}
→ {"role": "user", "content": "สวัสดี"}
```

Build ผ่าน ทดสอบกับ Lesson 1 (Hello Agent) — ทำงานได้ ตอบ text ธรรมดา

---

## Bug #1: Tool schemas ว่างหมด

**อาการ:** รัน Lesson 2 (Tools) แล้ว model ไม่เรียก tool เลย ตอบตรงๆ ว่าไม่รู้เวลา

**debug:** เพิ่ม log พิมพ์ request ที่ส่งออกไป พบว่า:

```json
"tools": [
  {"name": "get_time", "parameters": {"type": "object", "properties": {}}}
]
```

`properties` ว่าง! Model ไม่รู้ว่า tool รับ argument อะไร

**root cause:** code เดิมอ่าน `fd.Parameters` ซึ่งเป็น `*genai.Schema`  
แต่ ADK v1.4 เก็บ schema ไว้ใน `fd.ParametersJsonSchema` (raw `interface{}`) แทน

ค้นพบโดยดู source code ของ `functiontool/function.go`:
```go
func (f *functionTool[...]) Declaration() *genai.FunctionDeclaration {
    decl := &genai.FunctionDeclaration{...}
    if f.inputSchema != nil {
        decl.ParametersJsonSchema = f.inputSchema.Schema()  // ← ตรงนี้
    }
    ...
}
```

**fix:** เปลี่ยนไปอ่าน `fd.ParametersJsonSchema` ก่อน fallback มา `fd.Parameters`

---

## Bug #2: System instruction หาย

**อาการ:** Agent ไม่ทำตาม instruction ที่กำหนด ตอบผิด tone

**root cause:** `req.Contents` ไม่มี system message — ADK เก็บ `Instruction` ไว้ใน
`req.Config.SystemInstruction` (เป็น `*genai.Content` แยกต่างหาก) ไม่ได้ใส่ใน conversation contents

**fix:** เพิ่ม `buildMessages()` ที่อ่าน `req.Config.SystemInstruction` แล้ว prepend เป็น
`{"role": "system", "content": "..."}` ก่อน messages อื่นๆ

---

## Bug #3: TurnComplete ผิด → tool ไม่ถูกเรียก

**อาการ:** Model ส่ง `tool_calls` กลับมา แต่ tool ไม่ run จริง — ADK stop ทันที

**root cause:** code เดิม:
```go
TurnComplete: choice.FinishReason == "stop" || choice.FinishReason == "tool_calls"
```

เมื่อ `finish_reason == "tool_calls"` → `TurnComplete = true` → ADK คิดว่า turn จบแล้ว ไม่ execute tool

**เข้าใจ ADK flow:**
```
model return FunctionCall parts
         ↓
TurnComplete = false  ← ต้องเป็น false
         ↓
ADK execute tools
         ↓
ADK ส่ง FunctionResponse กลับ model
         ↓
model return text response
         ↓
TurnComplete = true
```

**fix:** ตรวจจาก FunctionCall parts โดยตรง ไม่ใช่ finish_reason:
```go
hasFuncCall := content มี FunctionCall part หรือเปล่า
TurnComplete = !hasFuncCall && finish_reason ∈ {"stop", "length", ""}
```

เหตุผลที่ไม่ใช้ finish_reason: text-based models (ดู Bug #5) ใช้ `finish_reason: "stop"` แม้ขณะ call tool

---

## Bug #4: FunctionResponse สร้าง duplicate empty message

**อาการ:** หลัง tool run แล้ว model ตอบแปลกๆ ราวกับว่ามี empty message แทรกอยู่

**root cause:** ใน `contentsToMessages()`:

```go
// process FunctionResponse
for _, p := range c.Parts {
    if p.FunctionResponse != nil {
        msgs = append(msgs, ...)
    }
}
// ← ไม่มี continue!

// วิ่งต่อมาถึง text message section
text := ""  // empty เพราะ parts มีแต่ FunctionResponse ไม่มี Text
msgs = append(msgs, {Role: role, Content: ""})  // ← empty message ที่ไม่ต้องการ
```

**fix:** เพิ่ม `if hasFuncResp { continue }` หยุดไม่ให้ไปสร้าง empty message

---

## Bug #5: Text-based function calling (qwen, gemma)

**อาการ:** qwen3.5 / gemma4 ไม่เรียก tool ผ่าน proper OpenAI `tool_calls` field  
แต่แทรก JSON ลงใน `content` field แทน:

```json
{
  "finish_reason": "stop",
  "message": {
    "content": "{\"function_name\": \"add_item\", \"arguments\": {\"name\": \"apple\", \"qty\": 1}}",
    "tool_calls": null
  }
}
```

**ค้นพบวิธี:** test raw curl กับ model แล้วดู response จริง

**เข้าใจปัญหา:** model เหล่านี้ไม่ได้ implement OpenAI function calling protocol ครบ
แต่ "รู้จัก" function calling ผ่าน fine-tuning และ output เป็น JSON text แทน

**fix:** เพิ่ม `parseTextFunctionCall()` ใน `messageToContent()`:
```
ถ้า response ไม่มี tool_calls แต่ content เป็น JSON ที่มี:
  "function_name"/"name" + "arguments"/"args"
→ แปลงเป็น FunctionCall part แทน
```

รองรับทั้ง plain JSON และ markdown code fence:
```json
{"function_name": "...", "arguments": {...}}
{"name": "...", "arguments": {...}}
```
````
```json
{"name": "...", "arguments": {...}}
```
````

---

## Bug #6: FunctionResponse format สำหรับ text-based models

**อาการ:** หลัง tool run แล้วส่ง result กลับ model เป็น `{"role": "tool"}` message
แต่ qwen/gemma ไม่รู้จัก `tool` role → ตอบแบบ echo กลับ content หรือ confused

**root cause:** OpenAI `tool` role เป็น convention ที่ model ต้อง fine-tune มาเพื่อเข้าใจ
text-based models ที่ไม่ได้ train กับ protocol นี้จะไม่เข้าใจ

**fix:** ตรวจ FunctionCall ID — ถ้าขึ้นต้นด้วย `text-call-` (synthetic ID จาก parseTextFunctionCall)
ให้ส่ง result เป็น `user` message ในรูปแบบที่ model เข้าใจ:
```
[Tool result for add_item]: {"message": "added apple x1", "cart": [...]}
```

ID ปกติ (จาก proper tool_calls) ยังส่งเป็น `tool` role เหมือนเดิม

---

## Bug #7: Tool call infinite loop

**อาการ:** qwen3.5 หลังได้รับ tool result แล้ว ยังคง return `tool_calls` อีกแทนที่จะตอบ text
→ ADK วน loop ไม่หยุด

**ค้นพบ:** test raw curl ส่ง conversation ที่มี tool result แล้ว ดู response:
```json
{"finish_reason": "tool_calls", "message": {"tool_calls": [...]}}
```
model เรียก tool เดิมซ้ำ

**root cause:** model ไม่เข้าใจว่าควรจะ "จบ" หลังจากได้ tool result
อาจเป็นเพราะ fine-tuning ไม่ครอบคลุม multi-turn tool calling

**fix:** ถ้า conversation มี tool result แล้ว → ส่ง `tools: nil` ใน request ถัดไป
เมื่อ model ไม่เห็น tools → ถูกบังคับให้ตอบ text ธรรมดา

```go
if hasToolResult(msgs) {
    tools = nil  // force text response
}
```

---

## สรุปการเรียนรู้

### ADK internals ที่ต้องรู้

| ข้อเท็จจริง | ผลกระทบ |
|-------------|---------|
| Schema เก็บใน `ParametersJsonSchema` | ต้องอ่าน field นี้ ไม่ใช่ `Parameters` |
| System instruction อยู่ใน `req.Config` แยก | ต้อง prepend เองก่อน messages |
| `TurnComplete: false` → ADK execute tools | ตรวจจาก content parts ไม่ใช่ finish_reason |
| FunctionResponse ID ต้อง match FunctionCall ID | เวลาสร้าง synthetic ID ต้องสอดคล้องกัน |

### Model behaviors ที่เจอ

| Model | Function Calling Style | ปัญหา |
|-------|----------------------|-------|
| GPT-4o, Claude | proper `tool_calls` field | ทำงานได้ดี |
| qwen3.5 | proper `tool_calls` แต่ loop ไม่หยุด | ต้องตัด tools หลัง first result |
| qwen/gemma รุ่นเล็ก | JSON ใน `content` text | ต้องใช้ text-based parsing |
| gemma4 | JSON ใน `content` แต่ wrap คำตอบใน JSON ด้วย | ตอบยังไม่ ideal |

### Design decisions

**ทำไม tool result เป็น `user` message สำหรับ text-based models?**  
เพราะ model เหล่านี้เรียนมาในรูปแบบ conversation ปกติ
`user`/`assistant` role เป็นสิ่งที่รู้จัก
`tool` role เป็น concept ที่ต้อง fine-tune โดยเฉพาะ

**ทำไมตัด `tools` แทนที่จะแก้ system prompt?**  
ง่ายและ reliable กว่า — ไม่มีทางที่ model จะเรียก tool ที่ไม่มีอยู่ใน request

---

## Feature: Structured Output (response_format)

**เหตุผล:** ADK `OutputSchema` field ใน `llmagent.Config` ทำให้ ADK set `req.Config.ResponseSchema`
และ `req.Config.ResponseMIMEType = "application/json"` ก่อนเรียก model
แต่ `chatRequest` struct เดิมไม่มี `response_format` field → model ไม่รู้ว่าต้องตอบ JSON schema

**การค้นพบ:** ดู `internal/llminternal/basic_processor.go` พบ:
```go
req.Config.ResponseSchema = state.OutputSchema
req.Config.ResponseMIMEType = "application/json"
```

**fix:** เพิ่ม `ResponseFormat *responseFormat` ใน `chatRequest`
และ `buildResponseFormat(req)` ที่อ่าน `req.Config.ResponseSchema` → แปลงเป็น OpenAI `json_schema` format:

```go
type responseFormat struct {
    Type       string      `json:"type"`
    JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}
```

**ข้อสังเกต:** qwen3.5 ไม่ enforce `strict: true` จริง — model ตอบ JSON แต่อาจมี field พิเศษ
ต้องใส่ field list ชัดเจนใน Instruction ด้วยถึงจะได้ JSON ตรง schema

---

## Feature: Generation Parameters (temperature / top_p / top_k)

**เหตุผล:** `llmagent.Config.GenerateContentConfig` มี field `Temperature`, `TopP`, `TopK`
แต่ `chatRequest` struct เดิมไม่มี field เหล่านี้ → ค่าที่ set ไม่ถูก forward ไปยัง model เลย

**fix:** เพิ่ม 3 fields ใน `chatRequest` และอ่านจาก `req.Config` ก่อน marshal:

```go
// chatRequest
Temperature *float32 `json:"temperature,omitempty"`
TopP        *float32 `json:"top_p,omitempty"`
TopK        *float32 `json:"top_k,omitempty"`

// ใน GenerateContent
if req.Config != nil {
    payload.Temperature = req.Config.Temperature
    payload.TopP = req.Config.TopP
    payload.TopK = req.Config.TopK
}
```

**วิธีใช้:**
```go
myAgent, _ := llmagent.New(llmagent.Config{
    Model: model,
    GenerateContentConfig: &genai.GenerateContentConfig{
        Temperature: genai.Ptr[float32](0.2),
        TopP:        genai.Ptr[float32](0.9),
        TopK:        genai.Ptr[float32](40),
    },
})
```

**หมายเหตุ:** `top_k` ไม่ใช่ standard OpenAI field แต่ LiteLLM และ Ollama รองรับ

---

## ข้อจำกัดที่ยังมีอยู่

1. **Streaming ไม่รองรับ** — `stream bool` parameter ถูก ignore, ส่งเป็น single response เสมอ
2. **Multi-tool per turn** — ถ้า model เรียกหลาย tools พร้อมกัน อาจมี edge case ที่ยังไม่ handle
3. **Text-based parsing เปราะ** — ถ้า model เปลี่ยน JSON format ก็หลุด
4. **Token usage** — `UsageMetadata` ไม่ถูก map กลับมา (ทำให้ AfterModelCallback ใน Lesson 4 ไม่ได้ token count จริง)
5. **Model quality** — local models ส่วนใหญ่ function calling ยังสู้ GPT-4/Claude ไม่ได้

---

## วิธีใช้

```go
import "github.com/pallat/adk/litellm"

model := litellm.New(
    os.Getenv("LITELLM_MODEL"),    // "gpt-4o", "qwen3.5", "ollama/llama3.2"
    os.Getenv("LITELLM_BASE_URL"), // "http://localhost:4000" (no /v1 suffix)
    os.Getenv("LITELLM_API_KEY"),
)

// ใช้แทน gemini.NewModel() ทุกที่
myAgent, _ := llmagent.New(llmagent.Config{
    Model: model,
    ...
})
```

> **Note:** `LITELLM_BASE_URL` ไม่ต้องใส่ `/v1` ท้าย เพราะ adapter เติม `/chat/completions` เอง

package memory

const extractionSystemPrompt = "You are a memory extraction system. Extract stable, reusable memories from the provided conversation context. Do not include secrets, credentials, or one-time codes. Output strict JSON only. If no memories, return {\"memories\":[]}."

const extractionUserTemplate = `Summary:
{{summary}}

Recent messages:
{{recent}}

Current turn:
User: {{user}}
Assistant: {{assistant}}

Return JSON: {"memories":[{"memory":"...","type":"...","importance":1,"confidence":0.0,"tags":[""],"source":"user"}]}`

const updateSystemPrompt = "You are a memory update system. Decide whether to ADD, UPDATE, DELETE, or NOOP based on a candidate memory and existing memories. Output strict JSON only."

const updateUserTemplate = `Candidate:
{{candidate}}

Existing memories:
{{existing}}

Return JSON: {"action":"ADD|UPDATE|DELETE|NOOP","target_id":0,"memory":"...","type":"...","reason":""}`

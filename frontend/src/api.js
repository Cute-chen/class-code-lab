export class APIError extends Error {
  constructor(message, status, data) {
    super(message)
    this.status = status
    this.data = data
  }
}

export async function api(path, options = {}) {
  const headers = new Headers(options.headers || {})
  const body = options.body
  if (body && !(body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(path, {
    credentials: 'include',
    ...options,
    headers,
    body: body && !(body instanceof FormData) && typeof body !== 'string' ? JSON.stringify(body) : body,
  })
  const contentType = response.headers.get('content-type') || ''
  const data = contentType.includes('application/json') ? await response.json() : await response.text()
  if (!response.ok) {
    throw new APIError(data?.error || data || '请求失败', response.status, data)
  }
  return data
}

export async function streamAI(payload, handlers, signal) {
  const response = await fetch('/api/student/ai/messages', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  })
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new APIError(data.error || 'AI 请求失败', response.status, data)
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const packets = buffer.split('\n\n')
    buffer = packets.pop() || ''
    for (const packet of packets) {
      let event = 'message'
      let data = ''
      for (const line of packet.split('\n')) {
        if (line.startsWith('event:')) event = line.slice(6).trim()
        if (line.startsWith('data:')) data += line.slice(5).trim()
      }
      if (!data) continue
      const parsed = JSON.parse(data)
      handlers[event]?.(parsed)
    }
  }
}

export const runnerURL = (token) => {
  const host = window.location.hostname
  return `${window.location.protocol}//${host}:8081/run/${encodeURIComponent(token)}`
}

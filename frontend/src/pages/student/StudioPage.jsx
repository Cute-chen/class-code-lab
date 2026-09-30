import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { html } from '@codemirror/lang-html'
import { oneDark } from '@codemirror/theme-one-dark'
import { ArrowCounterClockwise, ArrowsOut, Check, Code, FloppyDisk, MagicWand, Minus, PaperPlaneTilt, Play, Plus, RocketLaunch, Robot, Sparkle, Stop, Trash, Warning, Wrench, X } from '@phosphor-icons/react'
import { api, runnerURL, streamAI } from '../../api'
import { Badge, Button, Dialog, EmptyState, Notice, Skeleton, formatTime } from '../../components/UI'

const starterCode = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>我的作品</title>
  <style>
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; font-family: system-ui; background: #eff6ff; color: #172033; }
    main { text-align: center; padding: 32px; }
    button { border: 0; border-radius: 12px; padding: 12px 20px; background: #2563eb; color: white; font-size: 16px; cursor: pointer; }
  </style>
</head>
<body>
  <main>
    <h1>你好，代码世界！</h1>
    <p id="message">点击按钮看看会发生什么。</p>
    <button onclick="document.querySelector('#message').textContent='你成功运行了第一段代码！'">点我试试</button>
  </main>
</body>
</html>`

const quickPrompts = [
  ['修复错误', Wrench, '请检查当前代码，修复可能导致作品无法运行的问题。'],
  ['优化画面', MagicWand, '请在不改变核心玩法的情况下优化当前作品的画面。'],
]

const ideaExamples = [
  {
    label: '愤怒的小鸟·闯关版',
    description: '拖拽发射、三关挑战、计分与重玩',
    prompt: '我想做一个“愤怒的小鸟·闯关版”网页游戏：拖拽弹弓上的小鸟瞄准，松手后发射；设置 3 个难度递增的关卡，每关有不同位置的小猪和障碍物；击中小猪加分，小鸟用完则本关失败；需要有下一关、重玩本关和重新开始按钮，并适配电脑和触屏操作。请生成完整的单文件 HTML。',
  },
  {
    label: '太空飞船·陨石躲避战',
    description: '方向键移动、60 秒生存、难度递增',
    prompt: '我想做一个“太空飞船·陨石躲避战”网页游戏：玩家用方向键或屏幕按钮控制飞船躲避陨石，坚持 60 秒就获胜；存活越久分数越高，陨石会逐渐变快；加入开始、暂停、失败重试和最高分显示，并设计有星空动态背景。请生成完整的单文件 HTML。',
  },
  {
    label: '垃圾分类·限时挑战',
    description: '拖拽分类、连续答对奖励、知识提示',
    prompt: '我想做一个“垃圾分类·限时挑战”网页游戏：随机出现生活垃圾，玩家把它拖进可回收物、厨余垃圾、有害垃圾或其他垃圾桶；限时 60 秒，答对加分，连续答对有额外奖励，答错要显示正确分类和知识提示；结束后展示成绩和重新挑战按钮。请生成完整的单文件 HTML。',
  },
  {
    label: '像素小屋·昼夜互动',
    description: '切换昼夜、点击家具、寻找隐藏彩蛋',
    prompt: '我想做一个“像素小屋·昼夜互动”网页作品：可以切换白天和夜晚，点击台灯、窗户、收音机、小猫等物品会有不同动画和声音提示；房间里藏 5 个可发现的互动彩蛋，并显示收集进度；整体使用温暖的像素画风，适配电脑和手机。请生成完整的单文件 HTML。',
  },
]

const DEFAULT_AI_PANEL_WIDTH = 32
const AI_PANEL_WIDTH_STORAGE_KEY = 'class-code-lab:studio-ai-panel-width'

function getSavedAIPanelWidth() {
  try {
    const saved = Number(window.localStorage.getItem(AI_PANEL_WIDTH_STORAGE_KEY))
    return Number.isFinite(saved) && saved >= 20 && saved <= 70 ? saved : DEFAULT_AI_PANEL_WIDTH
  } catch {
    return DEFAULT_AI_PANEL_WIDTH
  }
}

function renderInlineMarkdown(text, keyPrefix) {
  const tokens = text.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*|__[^_\n]+__|\*[^*\n]+\*|_[^_\n]+_)/g)
  return tokens.map((token, index) => {
    if (!token) return null
    const key = `${keyPrefix}-${index}`
    if (token.startsWith('`') && token.endsWith('`')) return <code key={key}>{token.slice(1, -1)}</code>
    if ((token.startsWith('**') && token.endsWith('**')) || (token.startsWith('__') && token.endsWith('__'))) return <strong key={key}>{token.slice(2, -2)}</strong>
    if ((token.startsWith('*') && token.endsWith('*')) || (token.startsWith('_') && token.endsWith('_'))) return <em key={key}>{token.slice(1, -1)}</em>
    return <Fragment key={key}>{token}</Fragment>
  })
}

function extractFirstJSONObject(text) {
  const start = text.indexOf('{')
  if (start < 0) return ''
  let depth = 0
  let inString = false
  let escaped = false
  for (let index = start; index < text.length; index += 1) {
    const character = text[index]
    if (inString) {
      if (escaped) escaped = false
      else if (character === '\\') escaped = true
      else if (character === '"') inString = false
      continue
    }
    if (character === '"') inString = true
    else if (character === '{') depth += 1
    else if (character === '}') {
      depth -= 1
      if (depth === 0) return text.slice(start, index + 1)
    }
  }
  return ''
}

function renderBarePatch(content) {
  const looksLikePatch = content.includes('"type"') && content.includes('"patch"') && content.includes('"replacements"')
  if (!looksLikePatch) return null
  const jsonText = extractFirstJSONObject(content)
  if (!jsonText) return [<p key="patch-loading"><em>正在生成增量修改…</em></p>]
  try {
    const patch = JSON.parse(jsonText)
    if (patch.type !== 'patch' || !Array.isArray(patch.replacements)) return null
    return [
      <p key="patch-summary">{patch.summary || `已生成 ${patch.replacements.length} 处增量修改。`}</p>,
      <details className="ai-code-disclosure" key="patch-details"><summary><span>查看 AI 增量修改细节</span><small>{patch.replacements.length} 处替换</small></summary><pre className="ai-code-block"><code>{JSON.stringify(patch, null, 2)}</code></pre></details>,
    ]
  } catch {
    return [<p key="patch-loading"><em>正在生成增量修改…</em></p>]
  }
}

function renderAIContent(content) {
  const barePatch = renderBarePatch(content)
  if (barePatch) return barePatch
  const lines = content.replace(/\r\n?/g, '\n').split('\n')
  const blocks = []
  let paragraph = []
  let listItems = []
  let listKind = ''
  let codeLines = null
  let codeLanguage = ''

  const flushParagraph = () => {
    if (!paragraph.length) return
    blocks.push(<p key={`paragraph-${blocks.length}`}>{paragraph.map((line, index) => <Fragment key={`line-${index}`}>{index ? <br /> : null}{renderInlineMarkdown(line, `inline-${index}`)}</Fragment>)}</p>)
    paragraph = []
  }
  const flushList = () => {
    if (!listItems.length) return
    const List = listKind === 'ordered' ? 'ol' : 'ul'
    blocks.push(<List key={`list-${blocks.length}`}>{listItems.map((item, index) => <li key={`item-${index}`}>{renderInlineMarkdown(item, `list-${index}`)}</li>)}</List>)
    listItems = []
    listKind = ''
  }
  const flushText = () => {
    flushParagraph()
    flushList()
  }

  lines.forEach((line) => {
    const fence = line.match(/^```\s*([\w+-]+)?\s*$/)
    if (fence) {
      if (codeLines) {
        const label = codeLanguage === 'json' ? '查看 AI 增量修改细节' : '查看 AI 返回的完整代码'
        blocks.push(<details className="ai-code-disclosure" key={`code-${blocks.length}`}><summary><span>{label}</span><small>{codeLines.length} 行</small></summary><pre className="ai-code-block"><code>{codeLines.join('\n')}</code></pre></details>)
        codeLines = null
        codeLanguage = ''
      } else {
        flushText()
        codeLines = []
        codeLanguage = (fence[1] || '').toLowerCase()
      }
      return
    }
    if (codeLines) {
      codeLines.push(line)
      return
    }
    if (!line.trim()) {
      flushText()
      return
    }
    const heading = line.match(/^(#{1,3})\s+(.+)$/)
    if (heading) {
      flushText()
      const Heading = { 1: 'h3', 2: 'h4', 3: 'h5' }[heading[1].length]
      blocks.push(<Heading key={`heading-${blocks.length}`}>{renderInlineMarkdown(heading[2], `heading-${blocks.length}`)}</Heading>)
      return
    }
    const bullet = line.match(/^\s*[-*]\s+(.+)$/)
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/)
    if (bullet || ordered) {
      const nextKind = ordered ? 'ordered' : 'unordered'
      flushParagraph()
      if (listKind && listKind !== nextKind) flushList()
      listKind = nextKind
      listItems.push((bullet || ordered)[1])
      return
    }
    if (line.match(/^>\s?/)) {
      flushText()
      blocks.push(<blockquote key={`quote-${blocks.length}`}>{renderInlineMarkdown(line.replace(/^>\s?/, ''), `quote-${blocks.length}`)}</blockquote>)
      return
    }
    flushList()
    paragraph.push(line)
  })

  if (codeLines) {
    const label = codeLanguage === 'json' ? '查看 AI 增量修改细节' : '查看 AI 返回的完整代码'
    blocks.push(<details className="ai-code-disclosure" key={`code-${blocks.length}`}><summary><span>{label}</span><small>{codeLines.length} 行</small></summary><pre className="ai-code-block"><code>{codeLines.join('\n')}</code></pre></details>)
  }
  flushText()
  return blocks
}

function MessageProposal({ proposal, onApply }) {
  const title = proposal.format === 'patch' ? '增量修改提案' : '完整代码提案'
  return <div className="proposal-card proposal-card-inline">
    <div className="proposal-title"><Code size={20} /><strong>{title}</strong><Badge tone={proposal.applied_at ? 'success' : proposal.safety_ok ? 'success' : 'danger'}>{proposal.applied_at ? '已应用' : proposal.safety_ok ? '安全检查通过' : '需要修改'}</Badge></div>
    {proposal.summary ? <p>{proposal.summary}</p> : null}
    {proposal.safety_issues ? <small>{proposal.safety_issues}</small> : null}
    <div><Button icon={Check} onClick={() => onApply(proposal)} disabled={!proposal.safety_ok}>{proposal.applied_at ? '重新应用此版本' : '应用此版本到编辑器'}</Button></div>
  </div>
}

function FullRetryCard({ message, onRetry, disabled }) {
  return <div className="proposal-card proposal-card-inline retry-full-card">
    <div className="proposal-title"><Warning size={20} /><strong>增量修改无法安全匹配</strong><Badge tone="warning">未应用</Badge></div>
    <p>{message.proposalWarning || 'AI 返回的部分原代码与编辑器中的版本不一致。可以改用完整 HTML 重新生成，避免部分应用损坏作品。'}</p>
    <div><Button icon={ArrowCounterClockwise} onClick={() => onRetry(message)} disabled={disabled}>重新生成完整代码</Button></div>
  </div>
}

export default function StudioPage() {
  const [loading, setLoading] = useState(true)
  const [work, setWork] = useState(null)
  const [code, setCode] = useState('')
  const [revisions, setRevisions] = useState([])
  const [safety, setSafety] = useState({ ok: false, issues: [] })
  const [remaining, setRemaining] = useState(0)
  const [saveState, setSaveState] = useState('saved')
  const [tab, setTab] = useState('code')
  const [runURL, setRunURL] = useState('')
  const [runKey, setRunKey] = useState(0)
  const [runtimeError, setRuntimeError] = useState('')
  const [previewFullscreenOpen, setPreviewFullscreenOpen] = useState(false)
  const [previewZoom, setPreviewZoom] = useState(1)
  const [aiPanelWidth, setAIPanelWidth] = useState(getSavedAIPanelWidth)
  const [resizingPanels, setResizingPanels] = useState(false)
  const [messages, setMessages] = useState([])
  const [conversations, setConversations] = useState([])
  const [proposals, setProposals] = useState([])
  const [conversationId, setConversationId] = useState(0)
  const [prompt, setPrompt] = useState('')
  const [aiState, setAIState] = useState('idle')
  const [aiConfigured, setAIConfigured] = useState(true)
  const [notice, setNotice] = useState(null)
  const [publishOpen, setPublishOpen] = useState(false)
  const [publishForm, setPublishForm] = useState({ title: '', description: '' })
  const [publishing, setPublishing] = useState(false)
  const [thumbnailCapture, setThumbnailCapture] = useState(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [conversationHistoryOpen, setConversationHistoryOpen] = useState(false)
  const [conversationHistoryLoading, setConversationHistoryLoading] = useState(false)
  const [newConversationOpen, setNewConversationOpen] = useState(false)
  const [clearingCode, setClearingCode] = useState(false)
  const abortRef = useRef(null)
  const chatListRef = useRef(null)
  const fullscreenPreviewRef = useRef(null)
  const studioRef = useRef(null)
  const followStreamRef = useRef(true)
  const skipNextAutosaveRef = useRef(false)
  const loadedRef = useRef(false)
  const thumbnailCaptureRef = useRef(null)
  const thumbnailCaptureTimerRef = useRef(null)

  const clampAIPanelWidth = useCallback((width) => {
    const containerWidth = studioRef.current?.getBoundingClientRect().width || window.innerWidth
    const minWidth = 320 / containerWidth * 100
    const maxWidth = (containerWidth - 488) / containerWidth * 100
    return Math.min(maxWidth, Math.max(minWidth, width))
  }, [])

  useEffect(() => {
    if (loading || !studioRef.current) return undefined
    const observer = new ResizeObserver(() => {
      if (studioRef.current?.getBoundingClientRect().width <= 1100) return
      setAIPanelWidth((current) => Math.round(clampAIPanelWidth(current) * 10) / 10)
    })
    observer.observe(studioRef.current)
    return () => observer.disconnect()
  }, [loading, clampAIPanelWidth])

  function updateAIPanelWidth(width) {
    const nextWidth = Math.round(clampAIPanelWidth(width) * 10) / 10
    setAIPanelWidth(nextWidth)
    try { window.localStorage.setItem(AI_PANEL_WIDTH_STORAGE_KEY, String(nextWidth)) } catch { /* Storage may be unavailable. */ }
  }

  function resizePanels(event) {
    const rect = studioRef.current?.getBoundingClientRect()
    if (!rect) return
    updateAIPanelWidth((event.clientX - rect.left) / rect.width * 100)
  }

  function startPanelResize(event) {
    if (event.button !== 0) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    setResizingPanels(true)
    resizePanels(event)
  }

  function stopPanelResize(event) {
    if (!event.currentTarget.hasPointerCapture(event.pointerId)) return
    event.currentTarget.releasePointerCapture(event.pointerId)
    setResizingPanels(false)
  }

  function handlePanelResizeKeyDown(event) {
    const step = event.shiftKey ? 10 : 2
    if (event.key === 'ArrowLeft') updateAIPanelWidth(aiPanelWidth - step)
    else if (event.key === 'ArrowRight') updateAIPanelWidth(aiPanelWidth + step)
    else if (event.key === 'Home') updateAIPanelWidth(0)
    else if (event.key === 'End') updateAIPanelWidth(100)
    else return
    event.preventDefault()
  }

  useEffect(() => {
    const cancelAIRequest = () => abortRef.current?.abort()
    window.addEventListener('pagehide', cancelAIRequest)
    return () => {
      window.removeEventListener('pagehide', cancelAIRequest)
      cancelAIRequest()
    }
  }, [])

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      const [workData, aiData] = await Promise.all([api('/api/student/work'), api('/api/student/ai/history')])
      setWork(workData.work)
      setCode(workData.work.draft_cleared ? '' : (workData.work.draft_code || starterCode))
      setRevisions(workData.revisions || [])
      setSafety(workData.safety)
      setRemaining(workData.ai_remaining)
      setAIConfigured(aiData.configured)
      setConversations(aiData.conversations || [])
      const latest = aiData.conversations?.[0]
      if (latest) { setConversationId(latest.id); setMessages(latest.messages || []) }
      setProposals(aiData.proposals || [])
      loadedRef.current = true
    } catch (error) { setNotice({ type: 'error', title: error.message }) } finally { setLoading(false) }
  }, [])

  useEffect(() => { loadAll() }, [loadAll])
  useEffect(() => {
    const listener = (event) => {
      if (event.data?.source !== 'class-code-lab-runner') return
      const pendingCapture = thumbnailCaptureRef.current
      if (event.data.type === 'runtime-error') {
        if (!pendingCapture || event.data.capture_id !== pendingCapture.id) setRuntimeError(event.data.value)
        return
      }
      if (!pendingCapture || event.data.capture_id !== pendingCapture.id) return
      if (event.data.type !== 'thumbnail-captured' && event.data.type !== 'thumbnail-error') return
      window.clearTimeout(thumbnailCaptureTimerRef.current)
      thumbnailCaptureRef.current = null
      setThumbnailCapture(null)
      if (event.data.type === 'thumbnail-captured') {
        api('/api/student/work/thumbnail', { method: 'POST', body: { image: event.data.value, version: pendingCapture.version } }).catch(() => {})
      }
    }
    window.addEventListener('message', listener)
    return () => {
      window.removeEventListener('message', listener)
      window.clearTimeout(thumbnailCaptureTimerRef.current)
    }
  }, [])
  useEffect(() => {
    if (!previewFullscreenOpen) return undefined
    const previousOverflow = document.body.style.overflow
    const previousActiveElement = document.activeElement
    const closeOnEscape = (event) => {
      if (event.key === 'Escape') setPreviewFullscreenOpen(false)
    }
    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', closeOnEscape)
    const focusFrame = window.requestAnimationFrame(() => fullscreenPreviewRef.current?.querySelector('[data-preview-exit]')?.focus())
    return () => {
      window.cancelAnimationFrame(focusFrame)
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', closeOnEscape)
      previousActiveElement?.focus?.()
    }
  }, [previewFullscreenOpen])
  useLayoutEffect(() => {
    if (!followStreamRef.current || !chatListRef.current) return
    chatListRef.current.scrollTop = chatListRef.current.scrollHeight
  }, [messages])

  const save = useCallback(async (source = 'manual', metadata = null) => {
    if (!work) return null
    setSaveState('saving')
    try {
      const data = await api('/api/student/work', { method: 'PUT', body: { title: metadata?.title ?? work.title, description: metadata?.description ?? work.description, code, source } })
      setWork(data.work); setSafety(data.safety); setSaveState('saved')
      return data.work
    } catch (error) { setSaveState('failed'); setNotice({ type: 'error', title: '保存失败', body: error.message }); return null }
  }, [code, work])

  useEffect(() => {
    if (!loadedRef.current || !work) return
    if (skipNextAutosaveRef.current) {
      skipNextAutosaveRef.current = false
      return
    }
    setSaveState('changed')
    const timer = setTimeout(() => save('autosave'), 1400)
    return () => clearTimeout(timer)
  }, [code])

  async function preview() {
    if (!await save('preview')) return
    setRuntimeError('')
    try { const data = await api('/api/student/work/preview-token', { method: 'POST' }); setRunURL(runnerURL(data.token)); setRunKey((value) => value + 1); setTab('preview') }
    catch (error) { setSafety(error.data?.safety || safety); setNotice({ type: 'error', title: error.message }) }
  }

  function stopPreview() {
    setRunURL('')
    setRuntimeError('')
    setPreviewFullscreenOpen(false)
  }

  function changePreviewZoom(change) {
    setPreviewZoom((value) => Math.min(1.5, Math.max(0.5, Math.round((value + change) * 10) / 10)))
  }

  function startThumbnailCapture(token, version) {
    if (!token) return
    window.clearTimeout(thumbnailCaptureTimerRef.current)
    const id = globalThis.crypto?.randomUUID?.() || `${version}-${Date.now()}-${Math.random().toString(36).slice(2)}`
    thumbnailCaptureRef.current = { id, version }
    setThumbnailCapture({ id, url: `${runnerURL(token)}?capture=${encodeURIComponent(id)}` })
    thumbnailCaptureTimerRef.current = window.setTimeout(() => {
      if (thumbnailCaptureRef.current?.id !== id) return
      thumbnailCaptureRef.current = null
      setThumbnailCapture(null)
    }, 15000)
  }

  async function publish() {
    const metadata = { title: publishForm.title.trim(), description: publishForm.description.trim() }
    if (!metadata.title || publishing) return
    setPublishing(true)
    const savedWork = await save('publish', metadata)
    if (!savedWork) { setPublishing(false); return }
    try {
      const data = await api('/api/student/work/publish', { method: 'POST' })
      setWork({ ...savedWork, is_published: true, published_version: data.version, published_at: data.published_at })
      setPublishOpen(false)
      startThumbnailCapture(data.capture_token, data.version)
      setNotice({ type: 'success', title: '作品已发布到本班广场', body: data.capture_token ? '正在自动截取运行首帧作为作品封面。' : '本次未能生成封面，广场会继续显示默认图标。' })
    } catch (error) { setSafety(error.data?.safety || safety); setNotice({ type: 'error', title: error.message }) }
    finally { setPublishing(false) }
  }

  function openPublish() {
    setPublishForm({ title: work.title || '', description: work.description || '' })
    setPublishOpen(true)
  }

  function beginNewConversation(notice = { type: 'info', title: '已新建对话', body: '发送第一条消息后，这段对话会自动保存。' }) {
    setConversationId(0)
    setMessages([])
    setPrompt('')
    followStreamRef.current = true
    setNewConversationOpen(false)
    setNotice(notice)
  }

  function startNewConversation() {
    if (aiState !== 'idle' || clearingCode || saveState === 'saving') return
    if (code.trim()) {
      setNewConversationOpen(true)
      return
    }
    clearCodeAndStartNewConversation()
  }

  function keepCodeAndStartNewConversation() {
    beginNewConversation({ type: 'info', title: '已新建对话并保留当前代码', body: '新对话不会携带旧聊天记录，但 AI 仍会收到编辑器中的当前作品代码。' })
  }

  async function clearCodeAndStartNewConversation() {
    if (!work || clearingCode || saveState === 'saving') return
    const previousCode = code
    setClearingCode(true)
    setSaveState('saving')
    skipNextAutosaveRef.current = true
    setCode('')
    try {
      const data = await api('/api/student/work', { method: 'PUT', body: { title: work.title, description: work.description, code: '', source: 'new-conversation-clear' } })
      setWork(data.work)
      setSafety(data.safety)
      setSaveState('saved')
      setRunURL('')
      setRuntimeError('')
      setPreviewFullscreenOpen(false)
      setTab('code')
      const refreshed = await api('/api/student/work').catch(() => null)
      if (refreshed) {
        setWork(refreshed.work)
        setRevisions(refreshed.revisions || [])
        setSafety(refreshed.safety)
      }
      beginNewConversation({ type: 'success', title: '已清空代码并新建对话', body: '旧代码已保存到历史版本，已发布的公开作品不受影响。现在可以让 AI 从空白代码生成新作品。' })
    } catch (error) {
      skipNextAutosaveRef.current = true
      setCode(previousCode)
      setSaveState('failed')
      setNotice({ type: 'error', title: '清空代码失败', body: error.message })
    } finally {
      setClearingCode(false)
    }
  }

  async function openConversationHistory() {
    if (aiState !== 'idle') return
    setConversationHistoryOpen(true)
    setConversationHistoryLoading(true)
    try {
      const data = await api('/api/student/ai/history')
      setConversations(data.conversations || [])
      setProposals(data.proposals || [])
      setRemaining(data.remaining)
      setAIConfigured(data.configured)
    } catch (error) { setNotice({ type: 'error', title: '历史对话加载失败', body: error.message }) }
    finally { setConversationHistoryLoading(false) }
  }

  function restoreConversation(conversation) {
    setConversationId(conversation.id)
    setMessages(conversation.messages || [])
    setPrompt('')
    followStreamRef.current = true
    setConversationHistoryOpen(false)
    setNotice({ type: 'success', title: '已恢复历史对话', body: conversation.title })
  }

  async function sendPrompt(text = prompt) {
    text = text.trim(); if (!text || aiState !== 'idle') return
    setPrompt(''); setNotice(null); setAIState('writing')
    followStreamRef.current = true
    const tempUser = { id: `user-${Date.now()}`, role: 'user', content: text }
    const tempAssistant = { id: `assistant-${Date.now()}`, role: 'assistant', content: '', streaming: true }
    setMessages((value) => [...value, tempUser, tempAssistant])
    abortRef.current = new AbortController()
    try {
      await streamAI({ conversation_id: conversationId, message: text }, {
        queue: (data) => setAIState(data.message || '正在排队'),
        status: (data) => setAIState(data.message || '正在处理'),
        delta: (data) => { setAIState('正在生成'); setMessages((value) => value.map((item) => item.id === tempAssistant.id ? { ...item, content: item.content + data.delta } : item)) },
        error: (data) => { throw new Error(data.message) },
        done: (data) => {
          setConversationId(data.conversation_id); setRemaining(data.remaining)
          setMessages((value) => value.map((item) => item.id === tempAssistant.id ? { ...item, id: data.message_id, streaming: false, status: data.retry_available ? 'proposal_invalid' : 'success', proposalWarning: data.proposal_warning } : item))
          if (data.proposal) setProposals((value) => [...value, data.proposal])
        },
      }, abortRef.current.signal)
    } catch (error) {
      if (error.name !== 'AbortError') setNotice({ type: 'error', title: error.message })
      setMessages((value) => value.flatMap((item) => {
        if (item.id !== tempAssistant.id) return [item]
        return item.content ? [{ ...item, streaming: false, failed: true }] : []
      }))
    } finally { setAIState('idle'); abortRef.current = null }
  }

  async function retryFull(message) {
    if (aiState !== 'idle' || !message?.id || String(message.id).includes('-')) return
    setNotice(null); setAIState('writing')
    followStreamRef.current = true
    const tempAssistant = { id: `retry-${Date.now()}`, role: 'assistant', content: '', streaming: true }
    setMessages((value) => [...value, tempAssistant])
    abortRef.current = new AbortController()
    try {
      await streamAI({ conversation_id: conversationId, retry_message_id: Number(message.id), force_full: true }, {
        queue: (data) => setAIState(data.message || '正在排队'),
        status: (data) => setAIState(data.message || '正在处理'),
        delta: (data) => { setAIState('正在生成'); setMessages((value) => value.map((item) => item.id === tempAssistant.id ? { ...item, content: item.content + data.delta } : item)) },
        error: (data) => { throw new Error(data.message) },
        done: (data) => {
          setRemaining(data.remaining)
          setMessages((value) => value.map((item) => item.id === tempAssistant.id ? { ...item, id: data.message_id, streaming: false, status: data.retry_available ? 'proposal_invalid' : 'success', proposalWarning: data.proposal_warning } : item))
          if (data.proposal) setProposals((value) => [...value, data.proposal])
        },
      }, abortRef.current.signal)
    } catch (error) {
      if (error.name !== 'AbortError') setNotice({ type: 'error', title: error.message })
      setMessages((value) => value.flatMap((item) => {
        if (item.id !== tempAssistant.id) return [item]
        return item.content ? [{ ...item, streaming: false, failed: true }] : []
      }))
    } finally { setAIState('idle'); abortRef.current = null }
  }

  async function applyProposal(proposal) {
    try {
      const data = await api(`/api/student/ai/proposals/${proposal.id}/apply`, { method: 'POST' })
      setCode((current) => {
        if (current === data.code) return current
        skipNextAutosaveRef.current = true
        return data.code
      })
      setSafety(data.safety); setSaveState('saved'); setProposals((value) => value.map((item) => item.id === proposal.id ? { ...item, applied_at: data.applied_at } : item)); setNotice({ type: 'success', title: proposal.applied_at ? '代码提案已再次应用' : '代码提案已应用，可以运行预览了' })
    } catch (error) { setNotice({ type: 'error', title: error.message }) }
  }

  async function restore(revision) {
    try { const data = await api(`/api/student/work/restore/${revision.id}`, { method: 'POST' }); setCode((current) => { if (current === data.code) return current; skipNextAutosaveRef.current = true; return data.code }); setSafety(data.safety); setSaveState('saved'); setHistoryOpen(false); setNotice({ type: 'success', title: '已恢复历史版本' }) }
    catch (error) { setNotice({ type: 'error', title: error.message }) }
  }

  const saveLabel = { saved: '已保存', saving: '保存中', changed: '有修改', failed: '保存失败' }[saveState]
  const proposalsByMessage = useMemo(() => new Map(proposals.map((proposal) => [String(proposal.message_id), proposal])), [proposals])
  const safetyPending = saveState === 'changed' || saveState === 'saving'
  if (loading) return <main className="studio-loading"><Skeleton rows={7} /></main>
  if (!work) return <main className="page-container"><EmptyState title="作品加载失败" description="请刷新页面再试。" /></main>

  return <main ref={studioRef} className={`studio-page${resizingPanels ? ' is-resizing' : ''}`} style={{ '--ai-panel-width': `${aiPanelWidth}%` }}>
    {notice ? <div className="toast-wrap"><Notice type={notice.type} title={notice.title} onClose={() => setNotice(null)}>{notice.body}</Notice></div> : null}
    <section className="ai-panel">
      <header><div className="ai-header-main"><div className="ai-title"><Robot size={23} weight="duotone" /><div><strong>AI 编程助手</strong><small>{aiConfigured ? `本节剩余 ${remaining} 次` : '当前未配置模型'}</small></div></div><Badge tone={aiConfigured && aiState === 'idle' ? 'success' : 'warning'}>{!aiConfigured ? '手动模式' : aiState === 'idle' ? '可以提问' : aiState === 'writing' ? '正在编写' : aiState}</Badge></div><div className="ai-header-actions"><button className="conversation-action-button" onClick={openConversationHistory} disabled={aiState !== 'idle'}><ArrowCounterClockwise size={16} weight="bold" />历史对话</button><button className="conversation-action-button" onClick={startNewConversation} disabled={aiState !== 'idle' || clearingCode || saveState === 'saving'}><Plus size={16} weight="bold" />{clearingCode ? '正在清空' : '新建对话'}</button></div></header>
      {!aiConfigured ? <Notice type="warning" title="模型服务未配置">你仍然可以粘贴代码、手动修改、预览和发布。</Notice> : null}
      <div className="quick-prompts">{quickPrompts.map(([label, Icon, text]) => <button key={label} onClick={() => sendPrompt(text)} disabled={!aiConfigured || aiState !== 'idle'}><Icon size={16} />{label}</button>)}</div>
      <div className="chat-list" ref={chatListRef} aria-live="polite" onScroll={(event) => { const list = event.currentTarget; followStreamRef.current = list.scrollHeight - list.scrollTop - list.clientHeight < 80 }}>
        {messages.length === 0 ? <div className="idea-guide"><span className="large-icon"><Sparkle size={31} weight="duotone" /></span><h2>想做一个什么作品？</h2><p>选择一个具体作品开始，也可以在下方继续修改玩法和画面要求。</p><div className="idea-examples">{ideaExamples.map((example) => <button key={example.label} onClick={() => setPrompt(example.prompt)}><strong>{example.label}</strong><small>{example.description}</small></button>)}</div></div> : messages.map((message) => {
          const proposal = message.role === 'assistant' ? proposalsByMessage.get(String(message.id)) : null
          const retryAvailable = message.role === 'assistant' && !proposal && (message.status === 'proposal_invalid' || message.proposalWarning)
          return <article key={message.id} className={`chat-message ${message.role}${message.streaming ? ' streaming' : ''}`}><span>{message.role === 'user' ? '我' : 'AI'}</span><div>{message.content ? message.role === 'assistant' ? <div className="ai-markdown">{renderAIContent(message.content)}</div> : message.content : <em>{message.id?.toString().startsWith('retry-') ? '正在重新生成完整代码...' : '正在思考...'}</em>}{proposal ? <MessageProposal proposal={proposal} onApply={applyProposal} /> : null}{retryAvailable ? <FullRetryCard message={message} onRetry={retryFull} disabled={aiState !== 'idle'} /> : null}</div></article>
        })}
      </div>
      <div className="chat-compose"><textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); sendPrompt() } }} placeholder="描述想法，或请 AI 修改当前作品..." disabled={!aiConfigured || aiState !== 'idle'} /><button onClick={() => aiState === 'idle' ? sendPrompt() : abortRef.current?.abort()} disabled={!aiConfigured || (aiState === 'idle' && !prompt.trim())} aria-label={aiState === 'idle' ? '发送' : '停止'}>{aiState === 'idle' ? <PaperPlaneTilt size={21} weight="fill" /> : <Stop size={21} weight="fill" />}</button></div>
    </section>
    <div
      className="studio-resizer"
      role="separator"
      aria-label="调整 AI 助手与代码预览的宽度"
      aria-orientation="vertical"
      aria-valuenow={Math.round(aiPanelWidth)}
      aria-valuemin={Math.round(clampAIPanelWidth(0))}
      aria-valuemax={Math.round(clampAIPanelWidth(100))}
      tabIndex={0}
      title="拖动调整左右宽度，双击恢复默认"
      onPointerDown={startPanelResize}
      onPointerMove={(event) => { if (event.currentTarget.hasPointerCapture(event.pointerId)) resizePanels(event) }}
      onPointerUp={stopPanelResize}
      onPointerCancel={stopPanelResize}
      onLostPointerCapture={() => setResizingPanels(false)}
      onDoubleClick={() => updateAIPanelWidth(DEFAULT_AI_PANEL_WIDTH)}
      onKeyDown={handlePanelResizeKeyDown}
    />
    <section
      ref={fullscreenPreviewRef}
      className={`work-panel${previewFullscreenOpen ? ' work-panel-preview-fullscreen' : ''}`}
      role={previewFullscreenOpen ? 'dialog' : undefined}
      aria-modal={previewFullscreenOpen ? 'true' : undefined}
      aria-label={previewFullscreenOpen ? '全屏游玩预览' : undefined}
    >
      <header className="work-toolbar">
        <div className="work-toolbar-leading">
          <div className="work-tabs">
            <button className={tab === 'code' ? 'active' : ''} onClick={() => { setTab('code'); setPreviewFullscreenOpen(false) }}><Code size={18} />代码</button>
            <button className={tab === 'preview' ? 'active' : ''} onClick={() => setTab('preview')}><Play size={18} />预览</button>
          </div>
          {tab === 'preview' ? <span className="preview-run-status">{runURL ? '作品正在独立沙箱中运行' : '点击上方“运行预览”加载作品'}</span> : null}
        </div>
        <div className="work-actions">
          <span className={`save-state ${saveState}`}>{saveLabel}</span>
          <Button variant="ghost" icon={ArrowCounterClockwise} onClick={() => setHistoryOpen(true)} disabled={!revisions.length}>历史版本</Button>
          <Button variant="secondary" icon={FloppyDisk} onClick={() => save('manual')}>保存</Button>
          <Button variant="secondary" icon={runURL ? ArrowCounterClockwise : Play} onClick={preview}>{runURL ? '重新运行' : '运行预览'}</Button>
          {runURL ? <Button variant="ghost" icon={Stop} onClick={stopPreview}>停止</Button> : null}
          {runURL && tab === 'preview' ? <div className="preview-zoom-controls" role="group" aria-label="预览缩放">
            <button className="preview-zoom-button" type="button" onClick={() => changePreviewZoom(-0.1)} disabled={previewZoom <= 0.5} aria-label="缩小预览" title="缩小预览"><Minus size={17} weight="bold" aria-hidden="true" /></button>
            <button className="preview-zoom-value" type="button" onClick={() => setPreviewZoom(1)} aria-label={`当前缩放 ${Math.round(previewZoom * 100)}%，点击恢复 100%`} title="恢复 100%">{Math.round(previewZoom * 100)}%</button>
            <button className="preview-zoom-button" type="button" onClick={() => changePreviewZoom(0.1)} disabled={previewZoom >= 1.5} aria-label="放大预览" title="放大预览"><Plus size={17} weight="bold" aria-hidden="true" /></button>
          </div> : null}
          {runURL && tab === 'preview' ? previewFullscreenOpen
            ? <Button variant="secondary" icon={X} data-preview-exit onClick={() => setPreviewFullscreenOpen(false)}>退出全屏</Button>
            : <Button icon={ArrowsOut} onClick={() => setPreviewFullscreenOpen(true)}>全屏游玩</Button>
          : null}
          <Button icon={RocketLaunch} onClick={openPublish}>发布</Button>
        </div>
      </header>
      <div className="editor-stage">
        {tab === 'code' ? <CodeMirror className="code-editor" value={code} height="100%" theme={oneDark} extensions={[html()]} onChange={setCode} basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true }} /> : (
          <div className="preview-stage">
            <div className="preview-content">
              {runtimeError ? <Notice type="error" title="作品运行时出现错误">{runtimeError}</Notice> : null}
              <div className="preview-canvas">
                {runURL ? <iframe key={runKey} src={runURL} sandbox="allow-scripts" allow="fullscreen" allowFullScreen title="作品预览" referrerPolicy="no-referrer" style={{ width: `${100 / previewZoom}%`, height: `${100 / previewZoom}%`, transform: `scale(${previewZoom})` }} /> : <EmptyState icon={Play} title="预览尚未启动" description="平台会先检查代码，再在隔离环境中运行。" action={<Button icon={Play} onClick={preview}>开始预览</Button>} />}
              </div>
            </div>
          </div>
        )}
      </div>
      {!code.trim() ? <div className="safety-strip pending"><Warning size={18} weight="bold" /><div><strong>还没有代码</strong><span>请先编写或粘贴代码</span></div></div> : safetyPending ? <div className="safety-strip pending"><Warning size={18} weight="bold" /><div><strong>正在检查代码</strong><span>保存完成后会更新检查结果</span></div></div> : saveState === 'failed' ? <div className="safety-strip pending"><Warning size={18} weight="bold" /><div><strong>代码检查结果暂不可用</strong><span>请重试保存后再预览或发布</span></div></div> : !safety.ok ? <div className="safety-strip"><X size={18} weight="bold" /><div><strong>代码安全检查未通过</strong><span>{safety.issues?.join('；')}</span></div></div> : <div className="safety-strip safe"><Check size={18} weight="bold" /><div><strong>代码安全检查通过</strong><span>可以继续预览或发布</span></div></div>}
    </section>
    {thumbnailCapture ? <iframe className="thumbnail-capture-frame" src={thumbnailCapture.url} sandbox="allow-scripts allow-same-origin" title="正在生成作品封面" referrerPolicy="no-referrer" aria-hidden="true" /> : null}
    <Dialog open={newConversationOpen} title="新建对话" onClose={() => { if (!clearingCode) setNewConversationOpen(false) }} actions={<><Button variant="ghost" onClick={() => setNewConversationOpen(false)} disabled={clearingCode}>取消</Button><Button variant="secondary" onClick={keepCodeAndStartNewConversation} disabled={clearingCode}>保留代码并新建</Button><Button icon={Trash} onClick={clearCodeAndStartNewConversation} disabled={clearingCode}>{clearingCode ? '正在清空...' : '一键清空并新建'}</Button></>}><div className="new-conversation-choice"><Notice type="warning" title="如果要重新生成作品，建议先清空代码">新对话不会携带旧聊天记录，但默认会把编辑器中的当前代码发给 AI。保留代码适合继续修改；清空代码适合从零生成新作品。</Notice><p>当前代码约 {code.length.toLocaleString('zh-CN')} 个字符。清空前会自动保存历史版本，{work.is_published ? '已发布的公开版本不会被清空。' : '之后可在“历史版本”中恢复。'}</p></div></Dialog>
    <Dialog open={conversationHistoryOpen} title="历史对话" onClose={() => setConversationHistoryOpen(false)}>{conversationHistoryLoading ? <Skeleton rows={4} /> : conversations.length === 0 ? <EmptyState title="还没有历史对话" description="发送第一条消息后，对话会自动保存在这里。" /> : <div className="conversation-history-list">{conversations.map((conversation) => <button key={conversation.id} className={Number(conversation.id) === Number(conversationId) ? 'active' : ''} onClick={() => restoreConversation(conversation)}><div><strong>{conversation.title || '未命名对话'}</strong><small>{conversation.messages?.length || 0} 条消息 · {formatTime(conversation.updated_at || conversation.created_at)}</small></div>{Number(conversation.id) === Number(conversationId) ? <Badge tone="info">当前对话</Badge> : <span>恢复</span>}</button>)}</div>}</Dialog>
    <Dialog open={publishOpen} title="发布作品" onClose={() => { if (!publishing) setPublishOpen(false) }} actions={<><Button variant="ghost" onClick={() => setPublishOpen(false)} disabled={publishing}>继续修改</Button><Button icon={RocketLaunch} onClick={publish} disabled={publishing || !publishForm.title.trim()}>{publishing ? '发布中...' : '确认发布'}</Button></>}><div className="publish-form"><label className="field"><span>作品名称</span><input value={publishForm.title} maxLength={40} autoFocus placeholder="给作品起一个名字" onChange={(event) => setPublishForm((value) => ({ ...value, title: event.target.value }))} /></label><label className="field"><span>作品简介</span><textarea value={publishForm.description} maxLength={200} placeholder="一句话介绍玩法、操作方式或作品亮点" onChange={(event) => setPublishForm((value) => ({ ...value, description: event.target.value }))} /></label><p className="field-help">名称必填，简介最多 200 字；这些内容会和当前代码一起发布。</p><Notice type={safetyPending ? 'warning' : safety.ok ? 'success' : 'error'} title={safetyPending ? '代码正在保存' : safety.ok ? '安全检查通过' : '暂时不能发布'}>{safetyPending ? '确认发布时会先保存并重新检查当前代码。' : safety.ok ? '发布后，本班同学可以在作品广场中体验。' : safety.issues?.join('；')}</Notice>{work.is_published ? <p>这次发布会更新现有作品的公开版本，当前草稿仍可继续修改。</p> : null}</div></Dialog>
    <Dialog open={historyOpen} title="历史版本" onClose={() => setHistoryOpen(false)}><div className="revision-list">{revisions.map((revision) => <div key={revision.id}><div><strong>{revision.summary}</strong><small>{formatTime(revision.created_at)}，来源：{revision.source}</small></div><Button variant="secondary" onClick={() => restore(revision)}>恢复</Button></div>)}</div></Dialog>
  </main>
}

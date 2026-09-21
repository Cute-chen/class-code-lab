import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, ArrowsOut, ArrowCounterClockwise, Coins, Play, Stop, Trophy, UsersThree } from '@phosphor-icons/react'
import { api, runnerURL } from '../../api'
import ScoreControl from '../../components/ScoreControl'
import { Badge, Button, EmptyState, Notice, Skeleton } from '../../components/UI'

const awardName = (rank) => ({ 1: '冠军', 2: '亚军', 3: '季军' }[rank] || '')

export default function WorkPage() {
  const { id } = useParams()
  const [work, setWork] = useState(null)
  const [scoring, setScoring] = useState(null)
  const [error, setError] = useState('')
  const [url, setURL] = useState('')
  const [key, setKey] = useState(0)
  const [runtimeError, setRuntimeError] = useState('')

  useEffect(() => {
    api(`/api/gallery/${id}`)
      .then((data) => { setWork(data.work); setScoring(data.scoring || null) })
      .catch((err) => setError(err.message))
  }, [id])
  useEffect(() => {
    const listener = (event) => { if (event.data?.source === 'class-code-lab-runner' && event.data.type === 'runtime-error') setRuntimeError(event.data.value) }
    window.addEventListener('message', listener)
    return () => window.removeEventListener('message', listener)
  }, [])

  async function run() {
    setRuntimeError('')
    try {
      const data = await api(`/api/gallery/${id}/run-token`, { method: 'POST' })
      setURL(runnerURL(data.token))
      setKey((value) => value + 1)
    } catch (err) {
      setError(err.message)
    }
  }
  function fullScreen() { document.querySelector('.experience-frame')?.requestFullscreen?.() }
  function applyScoreResponse(data) {
    const updated = data.works?.find((item) => item.id === Number(id))
    if (updated) setWork(updated)
    setScoring(data.scoring)
  }

  if (error && !work) return <main className="page-container"><EmptyState title="作品当前不可用" description={error} action={<Link to="/gallery"><Button icon={ArrowLeft}>返回广场</Button></Link>} /></main>
  if (!work) return <main className="page-container"><Skeleton rows={6} /></main>
  return <main className="experience-page">
    <header>
      <Link to="/gallery" className="back-link"><ArrowLeft size={19} />返回广场</Link>
      <div><h1>{work.title}</h1><p>{work.author}，{work.class_name}</p></div>
      <div><Button variant="secondary" icon={ArrowCounterClockwise} onClick={run}>重新运行</Button><Button variant="secondary" icon={Stop} onClick={() => setURL('')}>停止</Button><Button icon={ArrowsOut} onClick={fullScreen} disabled={!url}>全屏体验</Button></div>
    </header>
    {runtimeError ? <Notice type="error" title="作品运行时出现错误">{runtimeError}</Notice> : null}
    <section className="experience-frame">{url ? <iframe key={key} src={url} sandbox="allow-scripts" title={work.title} referrerPolicy="no-referrer" /> : <EmptyState icon={Play} title="准备开始体验" description="点击后，作品会在隔离环境中运行。" action={<Button icon={Play} onClick={run}>运行作品</Button>} />}</section>
    <aside className="experience-info">
      <div><strong>作品介绍</strong>{work.rank && work.rank <= 3 ? <Badge tone={work.rank === 1 ? 'warning' : 'info'}><Trophy size={14} weight="fill" />{awardName(work.rank)}</Badge> : null}</div>
      <p>{work.description || '作者还没有填写作品介绍。'}</p>
      {scoring ? <div className="experience-score-stats"><span><Coins size={17} />总积分 <strong>{work.score_total || 0}</strong></span><span><UsersThree size={17} />支持人数 <strong>{work.supporter_count || 0}</strong></span></div> : null}
      {scoring ? <ScoreControl work={work} scoring={scoring} onSaved={applyScoreResponse} /> : null}
    </aside>
  </main>
}

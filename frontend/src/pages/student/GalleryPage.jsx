import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Coins, GameController, MagnifyingGlass, Sparkle, Trophy, UsersThree } from '@phosphor-icons/react'
import { api } from '../../api'
import ScoreControl from '../../components/ScoreControl'
import { Badge, Button, EmptyState, PageHeader, Skeleton, formatTime } from '../../components/UI'

const awardName = (rank) => ({ 1: '冠军', 2: '亚军', 3: '季军' }[rank] || '')

function WorkCover({ item, colorIndex }) {
  const [imageFailed, setImageFailed] = useState(false)
  const showThumbnail = item.thumbnail_url && !imageFailed

  useEffect(() => setImageFailed(false), [item.thumbnail_url])

  return <div className={`work-cover cover-${colorIndex}${showThumbnail ? ' has-thumbnail' : ''}`}>
    {showThumbnail
      ? <img src={item.thumbnail_url} alt={`${item.title}的运行画面`} loading="lazy" onError={() => setImageFailed(true)} />
      : <GameController size={50} weight="duotone" />}
    {item.rank && item.rank <= 3 ? <span className="rank-medal"><Trophy size={17} weight="fill" />{awardName(item.rank)}</span> : null}
  </div>
}

function compareByScore(left, right) {
  if (left.rank || right.rank) {
    if (!left.rank) return 1
    if (!right.rank) return -1
    return left.rank - right.rank
  }
  return new Date(right.published_at || 0) - new Date(left.published_at || 0)
}

export default function GalleryPage({ mode }) {
  const [works, setWorks] = useState(null)
  const [scoring, setScoring] = useState(null)
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState(mode === 'class' ? 'score' : 'recent')

  useEffect(() => {
    setWorks(null)
    setScoring(null)
    setSort(mode === 'class' ? 'score' : 'recent')
    api(mode === 'featured' ? '/api/featured' : '/api/gallery')
      .then((data) => { setWorks(data.works); setScoring(data.scoring || null) })
      .catch(() => setWorks([]))
  }, [mode])

  const filtered = useMemo(() => (works || [])
    .filter((item) => `${item.title} ${item.author}`.toLowerCase().includes(search.toLowerCase()))
    .sort((left, right) => {
      if (sort === 'score') return compareByScore(left, right)
      if (sort === 'author') return left.author.localeCompare(right.author, 'zh-CN')
      return new Date(right.published_at || 0) - new Date(left.published_at || 0)
    }), [works, search, sort])

  function applyScoreResponse(data) {
    setWorks(data.works)
    setScoring(data.scoring)
  }

  return <main className="page-container gallery-page">
    <PageHeader title={mode === 'featured' ? '跨班精选' : '本班作品广场'} description={mode === 'featured' ? '体验教师挑选的优秀创意作品。' : `这里有 ${works?.length || 0} 个同学作品，体验后把评分积分送给喜欢的作品。`} />
    {mode === 'class' && scoring ? <section className={`score-summary${scoring.completed ? ' is-complete' : ''}`}>
      <div className="score-summary-icon"><Coins size={28} weight="duotone" /></div>
      <div><strong>{scoring.enabled ? '作品评分进行中' : '作品评分已暂停'}</strong><span>{scoring.enabled ? '已投出的积分会立即计入排行榜，可随时调整。' : '当前不能修改评分，已有排行榜会继续保留。'}</span></div>
      <dl>
        <div><dt>总额度</dt><dd>{scoring.budget}</dd></div>
        <div><dt>已使用</dt><dd>{scoring.spent}</dd></div>
        <div><dt>剩余</dt><dd>{scoring.remaining}</dd></div>
      </dl>
      {scoring.completed ? <Badge tone="success">已完成评分</Badge> : null}
    </section> : null}
    {works === null ? <Skeleton rows={6} /> : works.length === 0 ? <EmptyState icon={mode === 'featured' ? Sparkle : GameController} title={mode === 'featured' ? '还没有精选作品' : '还没有同学发布作品'} description={mode === 'featured' ? '教师推荐优秀作品后会出现在这里。' : '完成自己的作品并发布，成为广场里的第一个作品。'} action={mode === 'class' ? <Link to="/studio"><Button>去创作</Button></Link> : null} /> : <>
      <div className="gallery-tools">
        <label className="search-input"><MagnifyingGlass size={18} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索作品或作者" /></label>
        <select value={sort} onChange={(event) => setSort(event.target.value)} aria-label="作品排序">
          {mode === 'class' ? <option value="score">积分排行</option> : null}
          <option value="recent">最近发布</option>
          <option value="author">作者姓名</option>
        </select>
      </div>
      <div className="gallery-grid">{filtered.map((item, index) => <article className={`work-card${item.rank && item.rank <= 3 ? ` rank-${item.rank}` : ''}`} key={item.id}>
        <WorkCover item={item} colorIndex={index % 4} />
        <div className="work-card-body">
          <div className="work-card-title"><h2>{item.title}</h2>{item.is_mine ? <Badge tone="info">我的作品</Badge> : null}</div>
          <p>{item.description || '作者还没有填写简介，进入作品看看吧。'}</p>
          <dl>
            <div><dt>作者</dt><dd>{item.author}</dd></div>
            {mode === 'featured' ? <div><dt>班级</dt><dd>{item.class_name}</dd></div> : null}
            {mode === 'class' ? <><div><dt><Coins size={14} />总积分</dt><dd>{item.score_total || 0}</dd></div><div><dt><UsersThree size={14} />支持人数</dt><dd>{item.supporter_count || 0}</dd></div></> : null}
            <div><dt>发布</dt><dd>{formatTime(item.published_at)}</dd></div>
          </dl>
          {mode === 'class' ? <ScoreControl work={item} scoring={scoring} onSaved={applyScoreResponse} /> : null}
          <Link to={`/works/${item.id}`}><Button variant="secondary" icon={GameController}>开始体验</Button></Link>
        </div>
      </article>)}</div>
    </>}
  </main>
}

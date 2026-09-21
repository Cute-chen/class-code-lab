import { useEffect, useMemo, useState } from 'react'
import { Coins, Eye, GameController, Lock, MagnifyingGlass, Sparkle, Trophy, UsersThree } from '@phosphor-icons/react'
import { api, runnerURL } from '../../api'
import { useTeacher } from '../../contexts/TeacherContext'
import { Badge, Button, Dialog, EmptyState, Notice, PageHeader, Skeleton, formatTime } from '../../components/UI'
import WorkPreviewDialog from '../../components/WorkPreviewDialog'

const awardName = (rank) => ({ 1: '冠军', 2: '亚军', 3: '季军' }[rank] || '')

function TeacherWorkCover({ work, colorIndex }) {
  const [imageFailed, setImageFailed] = useState(false)
  const showThumbnail = work.thumbnail_url && !imageFailed

  useEffect(() => setImageFailed(false), [work.thumbnail_url])

  return <div className={`teacher-work-cover cover-${colorIndex}${showThumbnail ? ' has-thumbnail' : ''}`}>
    {showThumbnail
      ? <img src={work.thumbnail_url} alt={`${work.title || '未命名作品'}的运行画面`} loading="lazy" onError={() => setImageFailed(true)} />
      : <GameController size={48} weight="duotone" />}
    {work.rank ? <span className="teacher-rank-medal"><Trophy size={16} weight="fill" />{work.rank <= 3 ? awardName(work.rank) : `第 ${work.rank} 名`}</span> : null}
  </div>
}

function TeacherWorkCard({ work, podium = false, onPreview, onAction, onUnpublish }) {
  return <article className={`teacher-work-card${podium ? ' is-podium' : ''}${work.rank ? ` rank-${work.rank}` : ''}`}>
    <TeacherWorkCover work={work} colorIndex={work.id % 4} />
    <div className="teacher-work-card-body">
      <div className="teacher-work-card-title">
        <div><h3>{work.title || '未命名作品'}</h3><span>{work.author}</span></div>
        {work.rank ? <Badge tone="warning">{work.rank <= 3 ? awardName(work.rank) : `第 ${work.rank} 名`}</Badge> : null}
      </div>
      <p>{work.description || '暂无简介'}</p>
      {work.is_published ? <dl className="teacher-work-stats">
        <div><dt><Coins size={15} />总积分</dt><dd>{work.score_total || 0}</dd></div>
        <div><dt><UsersThree size={15} />支持人数</dt><dd>{work.supporter_count || 0}</dd></div>
      </dl> : null}
      <div className="badge-row">
        {work.is_published ? <Badge tone="success">已发布 V{work.published_version}</Badge> : <Badge>未展示</Badge>}
        {work.is_featured ? <Badge tone="info">跨班精选</Badge> : null}
        {work.is_locked ? <Badge tone="danger">已锁定</Badge> : null}
      </div>
      <small>最近修改：{formatTime(work.updated_at)}</small>
      <div className="admin-work-actions">
        <Button variant="ghost" icon={Eye} onClick={() => onPreview(work)}>安全预览</Button>
        <Button variant="ghost" icon={Lock} onClick={() => onAction(work, work.is_locked ? 'unlock' : 'lock')}>{work.is_locked ? '解锁' : '锁定'}</Button>
        {work.is_published
          ? <Button variant="secondary" onClick={() => onUnpublish(work)}>撤下</Button>
          : work.published_version > 0
            ? <Button variant="secondary" onClick={() => onAction(work, 'restore')}>恢复展示</Button>
            : null}
        <Button variant={work.is_featured ? 'ghost' : 'secondary'} icon={Sparkle} disabled={!work.is_published} onClick={() => onAction(work, work.is_featured ? 'unfeature' : 'feature')}>{work.is_featured ? '取消精选' : '加入精选'}</Button>
      </div>
    </div>
  </article>
}

export default function WorksPage() {
  const { selectedClassId, selectedClass } = useTeacher()
  const [works, setWorks] = useState(null)
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [confirm, setConfirm] = useState(null)
  const [preview, setPreview] = useState(null)
  const [notice, setNotice] = useState(null)

  const load = () => selectedClassId
    ? api(`/api/teacher/classes/${selectedClassId}/works`).then((data) => setWorks(data.works)).catch((err) => setNotice({ type: 'error', title: err.message }))
    : setWorks([])

  useEffect(() => { setWorks(null); load() }, [selectedClassId])

  const filtered = useMemo(() => (works || [])
    .filter((item) => `${item.title} ${item.author}`.toLowerCase().includes(search.toLowerCase()))
    .filter((item) => filter === 'all'
      || (filter === 'published' && item.is_published)
      || (filter === 'unpublished' && !item.is_published)
      || (filter === 'locked' && item.is_locked)
      || (filter === 'featured' && item.is_featured)), [works, search, filter])

  const groupedWorks = useMemo(() => ({
    podium: [2, 1, 3].map((rank) => filtered.find((work) => work.rank === rank)).filter(Boolean),
    ranked: filtered.filter((work) => work.rank >= 4),
    other: filtered.filter((work) => !work.rank),
  }), [filtered])

  async function action(work, actionName, note = '') {
    try {
      await api(`/api/teacher/works/${work.id}/${actionName}`, { method: 'POST', body: { note } })
      setConfirm(null)
      setNotice({ type: 'success', title: '作品状态已更新' })
      load()
    } catch (err) {
      setNotice({ type: 'error', title: err.message })
    }
  }

  async function previewWork(work) {
    try {
      const data = await api(`/api/teacher/works/${work.id}/run-token`, { method: 'POST' })
      setPreview({ work, url: runnerURL(data.token) })
    } catch (err) {
      setNotice({ type: 'error', title: err.message })
    }
  }

  const renderCard = (work, podium = false) => <TeacherWorkCard
    key={work.id}
    work={work}
    podium={podium}
    onPreview={previewWork}
    onAction={action}
    onUnpublish={(item) => setConfirm({ work: item, action: 'unpublish' })}
  />

  return <div className="teacher-page">
    <PageHeader title="作品管理" description={selectedClass ? `查看并管理 ${selectedClass.name} 的作品、排名与发布状态。` : '请先选择班级。'} />
    {notice ? <Notice type={notice.type} title={notice.title} onClose={() => setNotice(null)} /> : null}
    {!selectedClassId
      ? <EmptyState title="没有选中班级" description="创建并选择班级后查看作品。" />
      : works === null
        ? <Skeleton rows={6} />
        : <section className="teacher-section teacher-works-section">
          <div className="table-tools">
            <label className="search-input"><MagnifyingGlass size={18} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索作品或作者" /></label>
            <select value={filter} onChange={(event) => setFilter(event.target.value)}>
              <option value="all">全部作品</option>
              <option value="published">已发布</option>
              <option value="unpublished">未发布或已撤下</option>
              <option value="locked">已锁定</option>
              <option value="featured">跨班精选</option>
            </select>
          </div>
          {works.length === 0
            ? <EmptyState icon={Trophy} title="还没有学生作品" description="学生保存第一份草稿后会出现在这里。" />
            : filtered.length === 0
              ? <EmptyState title="没有符合条件的作品" description="可以更换筛选条件或搜索关键词。" />
              : <div className="teacher-work-groups">
                {groupedWorks.podium.length > 0 ? <section className="teacher-work-group" aria-labelledby="ranking-title">
                  <header className="teacher-work-group-heading"><h2 id="ranking-title">作品排行榜</h2><p>按积分、支持人数和发布时间计算名次。</p></header>
                  <div className="teacher-podium">
                    {groupedWorks.podium.map((work) => <div className={`teacher-podium-item rank-${work.rank}`} key={work.id}>
                      {renderCard(work, true)}
                      <div className={`teacher-podium-step rank-${work.rank}`} aria-label={`第 ${work.rank} 名，${awardName(work.rank)}`}><strong>{work.rank}</strong><span>{awardName(work.rank)}</span></div>
                    </div>)}
                  </div>
                </section> : null}
                {groupedWorks.ranked.length > 0 ? <section className="teacher-work-group" aria-labelledby="other-ranks-title">
                  <header className="teacher-work-group-heading"><h2 id="other-ranks-title">其他排名</h2><p>第四名起的作品按当前名次继续排列。</p></header>
                  <div className="teacher-work-grid">{groupedWorks.ranked.map((work) => renderCard(work))}</div>
                </section> : null}
                {groupedWorks.other.length > 0 ? <section className="teacher-work-group" aria-labelledby="other-works-title">
                  <header className="teacher-work-group-heading"><h2 id="other-works-title">{groupedWorks.podium.length || groupedWorks.ranked.length ? '其他作品' : '作品列表'}</h2><p>未获得积分、未发布或暂不参与排名的作品。</p></header>
                  <div className="teacher-work-grid">{groupedWorks.other.map((work) => renderCard(work))}</div>
                </section> : null}
              </div>}
        </section>}
    <Dialog open={Boolean(confirm)} title="撤下作品" onClose={() => setConfirm(null)} actions={<><Button variant="ghost" onClick={() => setConfirm(null)}>取消</Button><Button variant="danger" onClick={() => action(confirm.work, confirm.action, confirm.note)}>确认撤下</Button></>}>
      <p>撤下后作品立即从广场消失，但学生草稿会保留。</p>
      <label className="field"><span>备注（可选）</span><textarea value={confirm?.note || ''} onChange={(event) => setConfirm((value) => ({ ...value, note: event.target.value }))} placeholder="例如：包含不合适内容，需要修改" /></label>
    </Dialog>
    <WorkPreviewDialog preview={preview} onClose={() => setPreview(null)} />
  </div>
}

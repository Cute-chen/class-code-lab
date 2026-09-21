package app

import "time"

const (
	RoleTeacher = "teacher"
	RoleStudent = "student"
)

type Class struct {
	ID                       uint      `gorm:"primaryKey" json:"id"`
	Name                     string    `gorm:"size:80;uniqueIndex;not null" json:"name"`
	Active                   bool      `gorm:"default:true" json:"active"`
	LoginOpen                bool      `gorm:"default:false" json:"login_open"`
	AIEnabled                bool      `gorm:"default:true" json:"ai_enabled"`
	PublishEnabled           bool      `gorm:"default:true" json:"publish_enabled"`
	AllowEditPublished       bool      `gorm:"default:true" json:"allow_edit_published"`
	AIRequestLimit           int       `gorm:"default:12" json:"ai_request_limit"`
	AIConcurrency            int       `gorm:"default:6" json:"ai_concurrency"`
	AIRequestCooldownSeconds int       `gorm:"default:3" json:"ai_request_cooldown_seconds"`
	ScoreBudget              int       `gorm:"default:0" json:"score_budget"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type User struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	ClassID            *uint      `gorm:"index:idx_class_login,unique" json:"class_id,omitempty"`
	Class              *Class     `json:"class,omitempty"`
	Role               string     `gorm:"size:20;index;not null" json:"role"`
	Name               string     `gorm:"size:80;not null" json:"name"`
	LoginName          string     `gorm:"size:80;index:idx_class_login,unique;not null" json:"login_name"`
	PasswordHash       string     `gorm:"size:255;not null" json:"-"`
	MustChangePassword bool       `gorm:"default:true" json:"must_change_password"`
	Locked             bool       `gorm:"default:false" json:"locked"`
	AIBlocked          bool       `gorm:"default:false" json:"ai_blocked"`
	AIExtraRequests    int        `gorm:"default:0" json:"ai_extra_requests"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type TeacherClassAccess struct {
	ID        uint `gorm:"primaryKey"`
	TeacherID uint `gorm:"uniqueIndex:idx_teacher_class"`
	ClassID   uint `gorm:"uniqueIndex:idx_teacher_class"`
}

type Session struct {
	ID        uint      `gorm:"primaryKey"`
	TokenHash string    `gorm:"size:64;uniqueIndex;not null"`
	UserID    uint      `gorm:"index;not null"`
	User      User      `gorm:"constraint:OnDelete:CASCADE"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
}

type Work struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	UserID             uint       `gorm:"uniqueIndex;not null" json:"user_id"`
	User               User       `json:"user,omitempty"`
	ClassID            uint       `gorm:"index;not null" json:"class_id"`
	Class              Class      `json:"class,omitempty"`
	Title              string     `gorm:"size:120" json:"title"`
	Description        string     `gorm:"size:500" json:"description"`
	DraftCode          string     `gorm:"type:mediumtext" json:"draft_code,omitempty"`
	DraftCleared       bool       `gorm:"default:false" json:"draft_cleared"`
	PublishedCode      string     `gorm:"type:mediumtext" json:"-"`
	PublishedVersion   int        `gorm:"default:0" json:"published_version"`
	Thumbnail          []byte     `gorm:"type:mediumblob" json:"-"`
	ThumbnailMediaType string     `gorm:"size:32" json:"-"`
	ThumbnailVersion   int        `gorm:"default:0" json:"thumbnail_version"`
	IsPublished        bool       `gorm:"default:false;index" json:"is_published"`
	IsFeatured         bool       `gorm:"default:false;index" json:"is_featured"`
	IsLocked           bool       `gorm:"default:false" json:"is_locked"`
	UnpublishedReason  string     `gorm:"size:500" json:"unpublished_reason,omitempty"`
	PublishedAt        *time.Time `json:"published_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type WorkRevision struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	WorkID    uint      `gorm:"index;not null" json:"work_id"`
	Source    string    `gorm:"size:30;not null" json:"source"`
	Summary   string    `gorm:"size:255" json:"summary"`
	Code      string    `gorm:"type:mediumtext" json:"code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkScore struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ClassID   uint      `gorm:"index;not null" json:"class_id"`
	VoterID   uint      `gorm:"index;uniqueIndex:idx_work_score_voter_work;not null" json:"voter_id"`
	WorkID    uint      `gorm:"index;uniqueIndex:idx_work_score_voter_work;not null" json:"work_id"`
	Points    int       `gorm:"not null" json:"points"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AIConversation struct {
	ID          uint        `gorm:"primaryKey" json:"id"`
	UserID      uint        `gorm:"index;not null" json:"user_id"`
	ClassID     uint        `gorm:"index;not null" json:"class_id"`
	ClassName   string      `gorm:"->;-:migration" json:"class_name,omitempty"`
	StudentName string      `gorm:"->;-:migration" json:"student_name,omitempty"`
	Title       string      `gorm:"size:120" json:"title"`
	Messages    []AIMessage `gorm:"foreignKey:ConversationID;constraint:OnDelete:CASCADE" json:"messages,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type AIMessage struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	ConversationID        uint      `gorm:"index;not null" json:"conversation_id"`
	Role                  string    `gorm:"size:20;not null" json:"role"`
	Content               string    `gorm:"type:mediumtext" json:"content"`
	Status                string    `gorm:"size:30" json:"status"`
	Model                 string    `gorm:"size:120" json:"model,omitempty"`
	LatencyMS             int64     `json:"latency_ms,omitempty"`
	PromptTokens          *int      `json:"prompt_tokens,omitempty"`
	CompletionTokens      *int      `json:"completion_tokens,omitempty"`
	PromptCacheHitTokens  *int      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int      `json:"prompt_cache_miss_tokens,omitempty"`
	FinishReason          string    `gorm:"size:40" json:"finish_reason,omitempty"`
	RequestMode           string    `gorm:"size:40" json:"request_mode,omitempty"`
	ReasoningEffort       string    `gorm:"size:20" json:"reasoning_effort,omitempty"`
	MaxOutputTokens       int       `json:"max_output_tokens,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}

type AICodeProposal struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	MessageID      uint       `gorm:"uniqueIndex;not null" json:"message_id"`
	ConversationID uint       `gorm:"index;not null" json:"conversation_id"`
	Code           string     `gorm:"type:mediumtext" json:"code,omitempty"`
	Summary        string     `gorm:"size:500" json:"summary"`
	Format         string     `gorm:"size:20" json:"format,omitempty"`
	SuggestedTitle string     `gorm:"size:120" json:"suggested_title"`
	SafetyOK       bool       `json:"safety_ok"`
	SafetyIssues   string     `gorm:"type:text" json:"safety_issues,omitempty"`
	AppliedAt      *time.Time `json:"applied_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AIUsageLog struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	UserID                uint      `gorm:"index;not null" json:"user_id"`
	ClassID               uint      `gorm:"index;not null" json:"class_id"`
	ClassName             string    `gorm:"->;-:migration" json:"class_name,omitempty"`
	StudentName           string    `gorm:"->;-:migration" json:"student_name,omitempty"`
	ConversationID        uint      `gorm:"index" json:"conversation_id"`
	Model                 string    `gorm:"size:120" json:"model"`
	Status                string    `gorm:"size:30;index" json:"status"`
	ErrorCategory         string    `gorm:"size:80" json:"error_category,omitempty"`
	DurationMS            int64     `json:"duration_ms"`
	PromptTokens          *int      `json:"prompt_tokens,omitempty"`
	CompletionTokens      *int      `json:"completion_tokens,omitempty"`
	PromptCacheHitTokens  *int      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int      `json:"prompt_cache_miss_tokens,omitempty"`
	FinishReason          string    `gorm:"size:40" json:"finish_reason,omitempty"`
	RequestMode           string    `gorm:"size:40" json:"request_mode,omitempty"`
	ReasoningEffort       string    `gorm:"size:20" json:"reasoning_effort,omitempty"`
	MaxOutputTokens       int       `json:"max_output_tokens,omitempty"`
	CreatedAt             time.Time `gorm:"index" json:"created_at"`
}

type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ActorID   *uint     `gorm:"index" json:"actor_id,omitempty"`
	ActorName string    `gorm:"size:80" json:"actor_name"`
	ClassID   *uint     `gorm:"index" json:"class_id,omitempty"`
	Target    string    `gorm:"size:120" json:"target"`
	Action    string    `gorm:"size:60;index" json:"action"`
	Result    string    `gorm:"size:30" json:"result"`
	Detail    string    `gorm:"size:500" json:"detail,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

type RunToken struct {
	ID        uint      `gorm:"primaryKey"`
	TokenHash string    `gorm:"size:64;uniqueIndex;not null"`
	WorkID    *uint     `gorm:"index"`
	ClassID   uint      `gorm:"index;not null"`
	Code      string    `gorm:"type:mediumtext;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
}

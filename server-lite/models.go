package main

// JSON shapes matching packages/core/types/*

type User struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type Workspace struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug"`
	Description *string                `json:"description"`
	Context     *string                `json:"context"`
	Settings    map[string]interface{} `json:"settings"`
	Repos       []WorkspaceRepo        `json:"repos"`
	IssuePrefix string                 `json:"issue_prefix"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

type WorkspaceRepo struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

type Member struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
}

type Project struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	IssueCount  int     `json:"issue_count"`
	DoneCount   int     `json:"done_count"`
}

type Issue struct {
	ID            string   `json:"id"`
	WorkspaceID   string   `json:"workspace_id"`
	Number        int      `json:"number"`
	Identifier    string   `json:"identifier"`
	Title         string   `json:"title"`
	Description   *string  `json:"description"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	CreatorType   string   `json:"creator_type"`
	CreatorID     string   `json:"creator_id"`
	ParentIssueID *string  `json:"parent_issue_id"`
	ProjectID     *string  `json:"project_id"`
	Position      float64  `json:"position"`
	DueDate       *string  `json:"due_date"`
	Reactions     []interface{} `json:"reactions"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type Comment struct {
	ID          string  `json:"id"`
	IssueID     string  `json:"issue_id"`
	CreatorType string  `json:"creator_type"`
	CreatorID   string  `json:"creator_id"`
	Content     string  `json:"content"`
	Type        string  `json:"type"`
	ParentID    *string `json:"parent_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type Agent struct {
	ID                string                 `json:"id"`
	WorkspaceID       string                 `json:"workspace_id"`
	RuntimeID         string                 `json:"runtime_id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Instructions      string                 `json:"instructions"`
	AvatarURL         *string                `json:"avatar_url"`
	RuntimeMode       string                 `json:"runtime_mode"`
	RuntimeConfig     map[string]interface{} `json:"runtime_config"`
	Visibility        string                 `json:"visibility"`
	Status            string                 `json:"status"`
	MaxConcurrentTasks int                   `json:"max_concurrent_tasks"`
	OwnerID           *string                `json:"owner_id"`
	Skills            []Skill                `json:"skills"`
	CreatedAt         string                 `json:"created_at"`
	UpdatedAt         string                 `json:"updated_at"`
	ArchivedAt        *string                `json:"archived_at"`
	ArchivedBy        *string                `json:"archived_by"`
}

type AgentRuntime struct {
	ID          string                 `json:"id"`
	WorkspaceID string                 `json:"workspace_id"`
	DaemonID    *string                `json:"daemon_id"`
	Name        string                 `json:"name"`
	RuntimeMode string                 `json:"runtime_mode"`
	Provider    string                 `json:"provider"`
	Status      string                 `json:"status"`
	DeviceInfo  string                 `json:"device_info"`
	Metadata    map[string]interface{} `json:"metadata"`
	OwnerID     *string                `json:"owner_id"`
	LastSeenAt  *string                `json:"last_seen_at"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

type AgentTask struct {
	ID           string      `json:"id"`
	AgentID      string      `json:"agent_id"`
	RuntimeID    string      `json:"runtime_id"`
	IssueID      string      `json:"issue_id"`
	Status       string      `json:"status"`
	Priority     int         `json:"priority"`
	DispatchedAt *string     `json:"dispatched_at"`
	StartedAt    *string     `json:"started_at"`
	CompletedAt  *string     `json:"completed_at"`
	Result       interface{} `json:"result"`
	Error        *string     `json:"error"`
	CreatedAt    string      `json:"created_at"`
}

type Skill struct {
	ID          string                 `json:"id"`
	WorkspaceID string                 `json:"workspace_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Content     string                 `json:"content"`
	Config      map[string]interface{} `json:"config"`
	Files       []SkillFile            `json:"files"`
	CreatedBy   *string                `json:"created_by"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

type SkillFile struct {
	ID        string `json:"id"`
	SkillID   string `json:"skill_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type InboxItem struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	UserID        string  `json:"user_id"`
	Type          string  `json:"type"`
	Read          bool    `json:"read"`
	Archived      bool    `json:"archived"`
	ReferenceID   *string `json:"reference_id"`
	ReferenceType *string `json:"reference_type"`
	Title         string  `json:"title"`
	Body          string  `json:"body"`
	CreatedAt     string  `json:"created_at"`
}

type PinnedItem struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	ItemType    string `json:"item_type"`
	ItemID      string `json:"item_id"`
	Position    int    `json:"position"`
	CreatedAt   string `json:"created_at"`
}

type PersonalAccessToken struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Name        string  `json:"name"`
	LastUsedAt  *string `json:"last_used_at"`
	CreatedAt   string  `json:"created_at"`
}

type TaskMessage struct {
	ID        string                 `json:"id"`
	TaskID    string                 `json:"task_id"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt string                 `json:"created_at"`
}

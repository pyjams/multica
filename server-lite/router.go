package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func newRouter(h *Handler) chi.Router {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:5174",
			"app://*",
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Workspace-ID", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": "lite"})
	})

	r.Get("/ws", h.handleWebSocket)

	// Public auth
	r.Post("/auth/send-code", h.handleSendCode)
	r.Post("/auth/verify-code", h.handleVerifyCode)
	r.Get("/auth/auto-login", h.handleAutoLogin)

	// Daemon API
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(h.daemonAuthMiddleware)
		r.Post("/register", h.handleDaemonRegister)
		r.Post("/deregister", h.handleDaemonDeregister)
		r.Post("/heartbeat", h.handleDaemonHeartbeat)

		r.Route("/runtimes/{runtimeId}", func(r chi.Router) {
			r.Get("/tasks/pending", h.handleListPendingTasks)
			r.Post("/tasks/claim", h.handleClaimTask)
			r.Post("/usage", h.handleReportRuntimeUsage)
			r.Post("/ping/{pingId}/result", h.handleReportPingResult)
			r.Post("/update/{updateId}/result", h.handleReportUpdateResult)
		})

		r.Route("/tasks/{taskId}", func(r chi.Router) {
			r.Get("/status", h.handleGetTaskStatus)
			r.Post("/start", h.handleStartTask)
			r.Post("/progress", h.handleReportProgress)
			r.Post("/complete", h.handleCompleteTask)
			r.Post("/fail", h.handleFailTask)
			r.Post("/usage", h.handleReportTaskUsage)
			r.Post("/messages", h.handleReportTaskMessages)
			r.Get("/messages", h.handleListTaskMessages)
		})
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(h.authMiddleware)

		r.Get("/api/me", h.handleGetMe)
		r.Patch("/api/me", h.handleUpdateMe)
		r.Post("/api/upload-file", h.handleUploadFile)

		r.Get("/api/assignee-frequency", h.handleGetAssigneeFrequency)

		// Workspaces
		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.handleListWorkspaces)
			r.Post("/", h.handleCreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.handleGetWorkspace)
				r.Put("/", h.handleUpdateWorkspace)
				r.Patch("/", h.handleUpdateWorkspace)
				r.Delete("/", h.handleDeleteWorkspace)
				r.Get("/members", h.handleListMembers)
				r.Post("/leave", h.handleLeaveWorkspace)
			})
		})

		// Tokens
		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.handleListTokens)
			r.Post("/", h.handleCreateToken)
			r.Delete("/{id}", h.handleRevokeToken)
		})

		// Issues
		r.Route("/api/issues", func(r chi.Router) {
			r.Get("/search", h.handleSearchIssues)
			r.Get("/", h.handleListIssues)
			r.Post("/", h.handleCreateIssue)
			r.Post("/batch-update", h.handleBatchUpdateIssues)
			r.Post("/batch-delete", h.handleBatchDeleteIssues)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.handleGetIssue)
				r.Put("/", h.handleUpdateIssue)
				r.Delete("/", h.handleDeleteIssue)
				r.Post("/comments", h.handleCreateComment)
				r.Get("/comments", h.handleListComments)
				r.Get("/timeline", h.handleListTimeline)
				r.Get("/subscribers", h.handleListIssueSubscribers)
				r.Post("/subscribe", h.handleSubscribeIssue)
				r.Post("/unsubscribe", h.handleUnsubscribeIssue)
				r.Get("/active-task", h.handleGetActiveTask)
				r.Post("/tasks/{taskId}/cancel", h.handleCancelTask)
				r.Get("/task-runs", h.handleListTasksByIssue)
				r.Get("/usage", h.handleGetIssueUsage)
				r.Post("/reactions", h.handleAddIssueReaction)
				r.Delete("/reactions", h.handleRemoveIssueReaction)
				r.Get("/attachments", h.handleListAttachments)
				r.Get("/children", h.handleListChildIssues)
			})
		})

		// Projects
		r.Route("/api/projects", func(r chi.Router) {
			r.Get("/search", h.handleSearchProjects)
			r.Get("/", h.handleListProjects)
			r.Post("/", h.handleCreateProject)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.handleGetProject)
				r.Put("/", h.handleUpdateProject)
				r.Delete("/", h.handleDeleteProject)
			})
		})

		// Pins
		r.Route("/api/pins", func(r chi.Router) {
			r.Get("/", h.handleListPins)
			r.Post("/", h.handleCreatePin)
			r.Put("/reorder", h.handleReorderPins)
			r.Delete("/{itemType}/{itemId}", h.handleDeletePin)
		})

		// Comments
		r.Route("/api/comments/{commentId}", func(r chi.Router) {
			r.Put("/", h.handleUpdateComment)
			r.Delete("/", h.handleDeleteComment)
			r.Post("/reactions", h.handleAddReaction)
			r.Delete("/reactions", h.handleRemoveReaction)
		})

		// Agents
		r.Route("/api/agents", func(r chi.Router) {
			r.Get("/", h.handleListAgents)
			r.Post("/", h.handleCreateAgent)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.handleGetAgent)
				r.Put("/", h.handleUpdateAgent)
				r.Post("/archive", h.handleArchiveAgent)
				r.Post("/restore", h.handleRestoreAgent)
				r.Get("/tasks", h.handleListAgentTasks)
				r.Get("/skills", h.handleListAgentSkills)
				r.Put("/skills", h.handleSetAgentSkills)
			})
		})

		// Skills
		r.Route("/api/skills", func(r chi.Router) {
			r.Get("/", h.handleListSkills)
			r.Post("/", h.handleCreateSkill)
			r.Post("/import", h.handleImportSkill)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.handleGetSkill)
				r.Put("/", h.handleUpdateSkill)
				r.Delete("/", h.handleDeleteSkill)
				r.Get("/files", h.handleListSkillFiles)
				r.Put("/files", h.handleUpsertSkillFile)
				r.Delete("/files/{fileId}", h.handleDeleteSkillFile)
			})
		})

		// Runtimes
		r.Route("/api/runtimes", func(r chi.Router) {
			r.Get("/", h.handleListRuntimes)
			r.Route("/{runtimeId}", func(r chi.Router) {
				r.Get("/usage", h.handleGetRuntimeUsage)
				r.Get("/activity", h.handleGetRuntimeActivity)
				r.Post("/ping", h.handleInitiatePing)
				r.Get("/ping/{pingId}", h.handleGetPing)
				r.Post("/update", h.handleInitiateUpdate)
				r.Get("/update/{updateId}", h.handleGetUpdate)
				r.Delete("/", h.handleDeleteRuntime)
			})
		})

		// Tasks
		r.Post("/api/tasks/{taskId}/cancel", h.handleCancelTaskByUser)

		// Inbox
		r.Route("/api/inbox", func(r chi.Router) {
			r.Get("/", h.handleListInbox)
			r.Get("/unread-count", h.handleCountUnreadInbox)
			r.Post("/mark-all-read", h.handleMarkAllInboxRead)
			r.Post("/archive-all", h.handleArchiveAllInbox)
			r.Post("/archive-all-read", h.handleArchiveAllReadInbox)
			r.Post("/archive-completed", h.handleArchiveCompletedInbox)
			r.Post("/{id}/read", h.handleMarkInboxRead)
			r.Post("/{id}/archive", h.handleArchiveInbox)
		})

		// Chat
		r.Route("/api/chat/sessions", func(r chi.Router) {
			r.Get("/", h.handleListChatSessions)
			r.Post("/", h.handleCreateChatSession)
			r.Route("/{sessionId}", func(r chi.Router) {
				r.Get("/", h.handleGetChatSession)
				r.Delete("/", h.handleArchiveChatSession)
				r.Get("/messages", h.handleListChatMessages)
				r.Post("/messages", h.handleSendChatMessage)
			})
		})
	})

	return r
}

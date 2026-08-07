package authz

const (
	ResourcePromptAudit = "prompt_audit"

	ActionPromptAuditViewFullPrompt = "view_full_prompt"
	ActionPromptAuditManage         = "manage"
	ActionPromptAuditDelete         = "delete"
)

var (
	PromptAuditRead           = Permission{Resource: ResourcePromptAudit, Action: ActionRead}
	PromptAuditViewFullPrompt = Permission{Resource: ResourcePromptAudit, Action: ActionPromptAuditViewFullPrompt}
	PromptAuditManage         = Permission{Resource: ResourcePromptAudit, Action: ActionPromptAuditManage}
	PromptAuditDelete         = Permission{Resource: ResourcePromptAudit, Action: ActionPromptAuditDelete}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourcePromptAudit,
		LabelKey: "Prompt audit",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read prompt audits",
				DescriptionKey: "View prompt audit lists, statistics, decisions, and redacted previews.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionPromptAuditViewFullPrompt,
				LabelKey:       "View full audited prompts",
				DescriptionKey: "View retained cleartext prompts in prompt audit details.",
			},
			{
				Action:         ActionPromptAuditManage,
				LabelKey:       "Manage prompt audit",
				DescriptionKey: "Change prompt audit configuration, test nodes, and retry failed jobs.",
			},
			{
				Action:         ActionPromptAuditDelete,
				LabelKey:       "Delete prompt audits",
				DescriptionKey: "Preview and delete terminal prompt audit records.",
			},
		},
	})
}

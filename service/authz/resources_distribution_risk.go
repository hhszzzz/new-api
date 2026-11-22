package authz

const (
	ResourceDistributionRisk     = "distribution_risk"
	ActionDistributionRiskManage = "manage"
)

var (
	DistributionRiskRead   = Permission{Resource: ResourceDistributionRisk, Action: ActionRead}
	DistributionRiskManage = Permission{Resource: ResourceDistributionRisk, Action: ActionDistributionRiskManage}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceDistributionRisk,
		LabelKey: "Distribution risk",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read distribution risk events",
				DescriptionKey: "View distribution risk events and client network evidence.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionDistributionRiskManage,
				LabelKey:       "Manage distribution risk",
				DescriptionKey: "Change detection settings and review distribution risk events.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}

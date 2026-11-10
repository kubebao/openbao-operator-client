package contracttest

import (
	"slices"
	"testing"
)

func canonicalRule(source, name, subject string) string {
	if source == "console" {
		if name == "recovery-investigate" {
			return map[string]string{"OpenBaoRestore": "KBAO-RESTORE-001", "OpenBaoRecovery": "KBAO-RECOVERY-001"}[subject]
		}
		return map[string]string{
			"replica-readiness": "KBAO-CLUSTER-003", "sealed": "KBAO-CLUSTER-002", "upgrade-failed": "KBAO-UPGRADE-001",
			"backup-failed": "KBAO-BACKUP-001", "backup-never-succeeded": "KBAO-BACKUP-002", "backup-overdue": "KBAO-BACKUP-003",
			"restore-test": "KBAO-RESTORETEST-001", "replica-history-conflict": "KBAO-REPLICATION-001", "collection": "coverage",
		}[name]
	}
	if source == "doctor" {
		return map[string]string{
			"KBAO-CLUSTER-001": "KBAO-CLUSTER-001", "KBAO-WORKLOAD-001": "KBAO-WORKLOAD-001", "KBAO-POD-001": "KBAO-POD-001",
			"KBAO-RESTRICTION-001": "KBAO-RESTRICTION-001", "KBAO-RESTORE-001": "KBAO-RESTORE-001",
		}[name]
	}
	if source != "attention" {
		return ""
	}
	if name == "reported_condition" {
		switch subject {
		case "Available":
			return "KBAO-CLUSTER-001"
		case "OpenBaoSealed":
			return "KBAO-CLUSTER-002"
		default:
			if slices.Contains([]string{
				"Degraded",
				"SecurityRisk",
				"EtcdEncryptionWarning",
				"NodeSecurityCapabilityMismatch",
				"TLSReady",
				"ACMEIntegrationReady",
				"ACMECacheReady",
				"GatewayIntegrationReady",
				"IngressIntegrationReady",
				"APIServerNetworkReady",
				"AuditFileStorageReady",
				"ProductionReady",
				"UserAccessBootstrap",
				"CloudUnsealIdentityReady",
				"BackupConfigurationReady",
				"StorageConfigured",
				"ReadReplicaStorageConfigured",
				"ReadReplicasReady",
				"ReadServingAvailable",
				"RaftMembershipReady",
				"ReadReplicasAutopilotHealthy",
				"OpenBaoInitialized",
				"OpenBaoLeader",
				"PolicyReconciliationReady",
			}, subject) {
				return "KBAO-CONDITION-001"
			}
			return ""
		}
	}
	return map[string]string{
		"generation_unknown": "coverage", "generation_mismatch": "KBAO-GENERATION-001", "availability_unknown": "coverage",
		"condition_generation_unconfirmed": "coverage", "paused": "KBAO-RESTRICTION-001", "maintenance": "KBAO-RESTRICTION-001",
		"restore_hold": "KBAO-RESTRICTION-001", "break_glass": "KBAO-RESTRICTION-001", "backup_failures": "KBAO-BACKUP-001",
		"controller_error": "KBAO-CONTROLLER-001", "upgrade_failure": "KBAO-UPGRADE-001",
	}[name]
}

func TestCanonicalRuleMappings(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string `json:"name"`
		Source  string `json:"source"`
		Legacy  string `json:"legacy"`
		Subject string `json:"subject"`
		Want    string `json:"want"`
	}](t, "rules")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, canonicalRule(tc.Source, tc.Legacy, tc.Subject), tc.Want) })
	}
}

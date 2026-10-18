package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// workGateCapacityMeasures measures every row of the register's "work_gate" area
// (capacity.AreaOf), one reading per row.
func (s *Server) workGateCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.WorkGateRoundDetailsPerItem: func() capacity.Reading {
			n, err := s.store.WorkGateRoundDetailsPerItemCount(context.Background())
			return countedCapacityReading(n, err, "fullest item in the retained gate round store")
		},
		capacity.WorkGateRoundDetailsPerStore: func() capacity.Reading {
			n, err := s.store.WorkGateRoundDetailsPerStoreCount(context.Background())
			return countedCapacityReading(n, err, "retained gate rounds")
		},
		capacity.WorkGateTasksPerRound: func() capacity.Reading {
			n, err := s.store.WorkGateTasksPerRoundCount(context.Background())
			return countedCapacityReading(n, err, "fullest persisted gate task lineage")
		},
		capacity.WorkGateClaimsPerRound: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-result admission guard; accepted claims persist with the round; no retained buffer exists for admission"}
		},
		capacity.WorkGateEvidenceStringsPerClaim: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-claim result admission guard; accepted evidence persists with the round; no retained buffer exists for admission"}
		},
		capacity.WorkGateEvidenceStringBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-evidence-string admission guard; accepted evidence persists with the round; no retained buffer exists for admission"}
		},
		capacity.WorkGateResultBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request byte guard; the accepted result persists with the round; no retained buffer exists for the request"}
		},
		capacity.WorkGateEvidenceArtifactsPerTask: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-task evidence admission guard; accepted artifacts persist as task evidence; no retained buffer exists for admission"}
		},
		capacity.WorkGateEvidenceArtifactBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request evidence byte guard; accepted artifacts persist as task evidence; no retained buffer exists for the request"}
		},
		capacity.WorkGateEvidenceTotalBytesPerTask: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-task evidence byte guard; accepted artifacts persist as task evidence; no retained buffer exists for admission"}
		},
		capacity.WorkGateRecentRoundsPerItemRead: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-answer window over durable rounds; no retained buffer"}
		},
		capacity.WorkGateDueRowsPerPass: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-pass window over the durable queue; no retained buffer"}
		},
		capacity.WorkGateRetryBackoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-retry timing ceiling; the due time persists; no retained buffer exists for timing"}
		},
		capacity.WorkGateOwnerOfflineGraceSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-escalation observation ceiling; escalation time persists; no retained buffer exists for observation"}
		},
	}
}

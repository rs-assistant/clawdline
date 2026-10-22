package http

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/logs"
	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// daemonCapacityMeasures measures every row of the register's "daemon" area
// (capacity.AreaOf), one reading per row.
func (s *Server) daemonCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.DeployHealthSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-deploy health guard; no retained wait"}
		},
		capacity.LogDaemon: func() capacity.Reading {
			w, ok := daemonLogs.Load(s.cfg.Dir)
			if !ok {
				return capacity.Unmeasured("this process writes its log to stderr, not to a file")
			}
			return w.(*logs.Writer).Reading()
		},
		capacity.DevicesList: func() capacity.Reading {
			g := s.gate()
			if g.files == nil {
				return capacity.Unmeasured(fmt.Sprint(g.err))
			}
			return g.files.DevicesReading()
		},
		capacity.DevicesOpenCodeSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-code expiry; no retained wait"}
		},
		capacity.DevicesOpenCodesPending: func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(s.gate().opens.waiting())}
		},
		capacity.PushSubscriptions: func() capacity.Reading {
			// Measuring does not make the push directory: a machine nobody
			// has asked to notify them holds no subscriptions, and that is a
			// known zero rather than a store opened to find out.
			if _, held := pushStores.Load(s.cfg.Dir); !held {
				_, err := os.Lstat(filepath.Join(s.cfg.Dir, adapterpush.DirName, adapterpush.SubscriptionsFile))
				if errors.Is(err, os.ErrNotExist) {
					return capacity.Reading{Known: true, Note: "nothing has subscribed on this machine"}
				}
			}
			store, err := s.push()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			reading := store.Reading()
			// A subscription made against Cloud's key can only be delivered
			// through Cloud, and a machine that is not signed in never falls
			// back to its own key, so it says here that it is holding rows it
			// cannot send to.
			if n := store.CloudSubscriptions(); n > 0 && s.pushCourier() == nil {
				reading.Note = fmt.Sprintf("%d subscription(s) made through Clawdline Cloud are undeliverable: this machine is not signed in to Cloud", n)
			}
			return reading
		},
		capacity.DiagnosticsRouteStatKeys: func() capacity.Reading { return s.routeStatsTable().keys() },
		capacity.DiagnosticsRouteLatencySamples: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-route ring"}
		},
		capacity.SettingsRequestBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.NotificationRulesRows: func() capacity.Reading {
			v, err := s.settingsFile().Read()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			rules, err := notificationRules(v)
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(len(rules))}
		},
		capacity.NotificationRuleLabel: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-rule admission guard; no retained buffer"}
		},
		capacity.NotificationRuleWait: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-rule delay guard; no retained buffer"}
		},
		capacity.AuditSecurity: func() capacity.Reading {
			g := s.gate()
			if g.files == nil {
				return capacity.Unmeasured(fmt.Sprint(g.err))
			}
			return g.files.AuditReading()
		},
		capacity.SSEScreenPending: func() capacity.Reading {
			if s.screenBus == nil {
				return capacity.Unmeasured("this server publishes no event stream")
			}
			return s.screenBus.reading()
		},
	}
}

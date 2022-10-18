package dns

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	utilmath "github.com/kuadrant/kcp-glbc/pkg/_internal/util/math"
)

// HostsWatcher keeps track of changes in host addresses in the background.
// It associates a host with a key that is passed to the `OnChange` callback
// whenever a change is detected
type HostsWatcher struct {
	Resolver      HostResolver
	Records       []RecordWatcher
	OnChange      func(interface{})
	WatchInterval func(ttl time.Duration) time.Duration
	logger        logr.Logger
}

func NewHostsWatcher(l *logr.Logger, resolver HostResolver, watchInterval func(ttl time.Duration) time.Duration) *HostsWatcher {
	return &HostsWatcher{
		Resolver:      resolver,
		Records:       []RecordWatcher{},
		WatchInterval: watchInterval,
		logger:        l.WithName("host-watcher"),
	}
}

func (w *HostsWatcher) ListHostRecordWatchers(obj interface{}) []RecordWatcher {
	var recordWatchers []RecordWatcher
	for _, record := range w.Records {
		if obj == record.key {
			recordWatchers = append(recordWatchers, record)
		}
	}
	return recordWatchers
}

// StartWatching begins tracking changes in the addresses for host
func (w *HostsWatcher) StartWatching(ctx context.Context, obj interface{}, host string) bool {
	for _, recordWatcher := range w.Records {
		if recordWatcher.key == obj && recordWatcher.Host == host {
			return false
		}
	}

	c, cancel := context.WithCancel(ctx)

	recordWatcher := RecordWatcher{
		cancel:        cancel,
		logger:        w.logger.WithValues("key", obj, "host", host),
		resolver:      w.Resolver,
		Host:          host,
		key:           obj,
		onChange:      w.OnChange,
		records:       []HostAddress{},
		watchInterval: w.WatchInterval,
		errInterval:   errorInterval,
	}
	recordWatcher.watch(c)

	w.Records = append(w.Records, recordWatcher)

	w.logger.V(3).Info("Started host watcher")
	return true
}

// StopWatching stops tracking changes in the addresses associated to obj
func (w *HostsWatcher) StopWatching(obj interface{}, host string) {
	var records []RecordWatcher
	for _, recordWatcher := range w.Records {
		if (host == "" || host == recordWatcher.Host) && recordWatcher.key == obj {
			recordWatcher.stop()
			continue
		}
		records = append(records, recordWatcher)
	}
	w.Records = records
}

type RecordWatcher struct {
	logger        logr.Logger
	resolver      HostResolver
	cancel        context.CancelFunc
	key           interface{}
	onChange      func(key interface{})
	watchInterval func(ttl time.Duration) time.Duration
	errInterval   time.Duration
	Host          string
	records       []HostAddress
}

var maxErrorInterval time.Duration = time.Minute * 5
var errorInterval time.Duration = time.Second * 2

func DefaultInterval(ttl time.Duration) time.Duration {
	return ttl / 2
}

func (w *RecordWatcher) watch(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			newRecords, err := w.resolver.LookupIPAddr(ctx, w.Host)
			if err != nil {
				w.logger.Error(err, "Failed to lookup IP address")
				sleepTime := w.errInterval
				w.errInterval = w.errInterval * w.errInterval
				time.Sleep(utilmath.Min(sleepTime, maxErrorInterval))

				continue
			}

			w.errInterval = errorInterval

			if updated := w.updateRecords(newRecords); updated {
				w.logger.V(3).Info("New records found")
				w.onChange(w.key)
			}

			ttl := w.records[0].TTL
			refreshInterval := w.watchInterval(ttl)
			time.Sleep(refreshInterval)
			w.logger.V(3).Info("Refreshing records for host", "TTL", int(ttl.Seconds()), "interval", int(refreshInterval.Seconds()))
		}
	}()
}

func (w *RecordWatcher) updateRecords(newRecords []HostAddress) bool {
	if len(w.records) != len(newRecords) {
		w.records = newRecords
		return true
	}

	updatedIPs := false
	updatedTTLs := false

	for i, newRecord := range newRecords {
		if !w.records[i].IP.Equal(newRecord.IP) {
			updatedIPs = true
			continue
		}

		if w.records[i].TTL < newRecord.TTL {
			updatedTTLs = true
		}
	}

	if updatedIPs || updatedTTLs {
		w.records = newRecords
	}

	return updatedIPs
}

func (w *RecordWatcher) stop() {
	w.logger.V(3).Info("Stopping host watcher")
	w.cancel()
}

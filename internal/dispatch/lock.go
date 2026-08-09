package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const dispatchLockPrefix = "gha-dispatch-"

// JobLocker serializes dispatch for a single workflow job ID across replicas.
type JobLocker interface {
	TryAcquire(ctx context.Context, jobID string) (release func(), acquired bool, err error)
}

// LeaseLocker uses a k8s Lease per job ID.
type LeaseLocker struct {
	client    kubernetes.Interface
	namespace string
	identity  string
	ttl       time.Duration
}

// NewLeaseLocker creates a JobLocker backed by coordination.k8s.io leases.
func NewLeaseLocker(client kubernetes.Interface, namespace, identity string, ttl time.Duration) *LeaseLocker {
	if ttl == 0 {
		ttl = 2 * time.Minute
	}
	if identity == "" {
		identity = defaultLockIdentity()
	}
	return &LeaseLocker{
		client:    client,
		namespace: namespace,
		identity:  identity,
		ttl:       ttl,
	}
}

func defaultLockIdentity() string {
	h, err := os.Hostname()
	if err != nil {
		return "gha-scheduler"
	}
	return h
}

// TryAcquire claims a per-job lease. acquired=false means another replica owns dispatch.
func (l *LeaseLocker) TryAcquire(ctx context.Context, jobID string) (func(), bool, error) {
	name := dispatchLockName(jobID)
	leases := l.client.CoordinationV1().Leases(l.namespace)

	existing, err := leases.Get(ctx, name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, false, fmt.Errorf("dispatch lock get: %w", err)
	}

	now := metav1.MicroTime{Time: time.Now()}
	ttlSec := int32(l.ttl.Seconds())

	if apierrors.IsNotFound(err) {
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: l.namespace},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &l.identity,
				LeaseDurationSeconds: &ttlSec,
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}
		if _, err := leases.Create(ctx, lease, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("dispatch lock create: %w", err)
		}
		return l.releaseFunc(ctx, name), true, nil
	}

	if holderActive(existing, l.ttl, now.Time) {
		return nil, false, nil
	}

	existing.Spec.HolderIdentity = &l.identity
	existing.Spec.LeaseDurationSeconds = &ttlSec
	existing.Spec.AcquireTime = &now
	existing.Spec.RenewTime = &now
	if _, err := leases.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return nil, false, fmt.Errorf("dispatch lock update: %w", err)
	}
	return l.releaseFunc(ctx, name), true, nil
}

func (l *LeaseLocker) releaseFunc(ctx context.Context, name string) func() {
	return func() {
		err := l.client.CoordinationV1().Leases(l.namespace).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			slog.Warn("dispatch lock release failed", "lease", name, "err", err)
		}
	}
}

func holderActive(lease *coordinationv1.Lease, ttl time.Duration, now time.Time) bool {
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return false
	}
	if lease.Spec.RenewTime == nil {
		return false
	}
	dur := ttl
	if lease.Spec.LeaseDurationSeconds != nil && *lease.Spec.LeaseDurationSeconds > 0 {
		dur = time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	}
	return now.Before(lease.Spec.RenewTime.Time.Add(dur))
}

func dispatchLockName(jobID string) string {
	return dispatchLockPrefix + jobID
}

// IsDispatchLocked reports whether an active dispatch lease exists for jobID.
func (l *LeaseLocker) IsDispatchLocked(ctx context.Context, jobID string) (bool, error) {
	name := dispatchLockName(jobID)
	existing, err := l.client.CoordinationV1().Leases(l.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("dispatch lock get: %w", err)
	}
	return holderActive(existing, l.ttl, time.Now()), nil
}

package dispatch_test

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/dispatch"
)

func TestLeaseLockerAcquireAndRelease(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	locker := dispatch.NewLeaseLocker(k8s, "gha-runners", "pod-a", time.Minute)

	release, acquired, err := locker.TryAcquire(context.Background(), "42")
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired")
	}

	lockerB := dispatch.NewLeaseLocker(k8s, "gha-runners", "pod-b", time.Minute)
	_, acquired2, err := lockerB.TryAcquire(context.Background(), "42")
	if err != nil {
		t.Fatalf("TryAcquire second: %v", err)
	}
	if acquired2 {
		t.Fatal("expected not acquired while held by pod-a")
	}

	release()

	_, acquired3, err := locker.TryAcquire(context.Background(), "42")
	if err != nil {
		t.Fatalf("TryAcquire after release: %v", err)
	}
	if !acquired3 {
		t.Fatal("expected acquired after release")
	}
}

func TestLeaseLockerExpiredLease(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	holder := "old-pod"
	renew := metav1.MicroTime{Time: time.Now().Add(-5 * time.Minute)}
	ttl := int32(30)
	_, _ = k8s.CoordinationV1().Leases("gha-runners").Create(context.Background(), &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "gha-dispatch-55", Namespace: "gha-runners"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			LeaseDurationSeconds: &ttl,
			RenewTime:            &renew,
		},
	}, metav1.CreateOptions{})

	locker := dispatch.NewLeaseLocker(k8s, "gha-runners", "pod-b", time.Minute)
	_, acquired, err := locker.TryAcquire(context.Background(), "55")
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected to take expired lease")
	}
}

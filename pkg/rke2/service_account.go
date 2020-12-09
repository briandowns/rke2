package rke2

import (
	"context"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	defaultServiceAccount = "default"

	serviceAccountNSAnnotation = "service-account.rke2.io"
)

// updateServiceAccountRef retrieves the most recent revision of given service account
// and updates the pointer to refer to the most recent revision. This get/change/update
// pattern is required to alter an object that may have changed since it was retrieved.
func updateServiceAccountRef(ctx context.Context, cs *kubernetes.Clientset, sa *v1.ServiceAccount, namespace string) error {
	logrus.Info("updating service account: " + sa.Name)
	newSA, err := cs.CoreV1().ServiceAccounts(namespace).Get(ctx, defaultServiceAccount, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*sa = *newSA
	return nil
}

// updateDefaultServiceAccount
func updateDefaultServiceAccount(ctx context.Context, cs *kubernetes.Clientset, ns *v1.Namespace) error {
	if _, ok := ns.Annotations[serviceAccountNSAnnotation]; !ok {
		sa, err := cs.CoreV1().ServiceAccounts(ns.Name).Get(ctx, defaultServiceAccount, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				logrus.Fatalf("service-account: update service account: %s - %s", ns.Name, err.Error())
			}
			logrus.Fatalf("service-account: update service account: %s - %s", ns.Name, err.Error())
		}
		var automountServiceAccountToken bool
		sa.AutomountServiceAccountToken = &automountServiceAccountToken
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if _, err := cs.CoreV1().ServiceAccounts(ns.Name).Update(ctx, sa, metav1.UpdateOptions{}); err != nil {
				if apierrors.IsConflict(err) {
					return updateServiceAccountRef(ctx, cs, sa, ns.Name)
				}
				return err
			}
			return nil
		}); err != nil {
			logrus.Fatalf("service-account: update service account: %s - %s", ns.Name, err.Error())
		}
		ns.Annotations[serviceAccountNSAnnotation] = cisAnnotationValue

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if _, err := cs.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
				if apierrors.IsConflict(err) {
					return updateNamespaceRef(ctx, cs, ns)
				}
				return err
			}
			return nil
		}); err != nil {
			logrus.Fatalf("networkPolicy: update namespace: %s - %s", ns.Name, err.Error())
		}
	}
	return nil
}

// updateDefaultServiceAccounts iterates the kube-system, kube-public, and default namespaces updating
// their default service accounts to not allow automounting of the service account token.
func updateDefaultServiceAccounts() func(context.Context, <-chan struct{}, string) error {
	return func(ctx context.Context, apiServerReady <-chan struct{}, kubeConfigAdmin string) error {
		go func() {
			<-apiServerReady
			logrus.Info("Updating default service accounts")

			cs, err := newClient(kubeConfigAdmin, nil)
			if err != nil {
				logrus.Fatalf("service-account: new k8s client: %s", err.Error())
			}

			for _, namespace := range namespaces {
				ns, err := cs.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
				if err != nil {
					logrus.Fatalf("service-account: get namespace %s: %s", namespace, err.Error())
				}
				if ns.Annotations == nil {
					ns.Annotations = make(map[string]string)
				}
				if err := updateDefaultServiceAccount(ctx, cs, ns); err != nil {
					logrus.Fatal(err)
				}
			}
			logrus.Info("Updating service accounts complete")
		}()

		return nil
	}
}

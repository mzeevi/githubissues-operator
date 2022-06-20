/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"math/rand"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/joho/godotenv"
	trainingv1alpha1 "github.com/mzeevi/githubissues-operator/api/v1alpha1"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	testRepo         = "https://github.com/mzeevi/githubissues-operator"
	testNamespace    = "default"
	charset          = "abcdefghijklmnopqrstuvwxyz" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	randStringLength = 10
	timeout          = time.Second * 10
	duration         = time.Second * 10
	interval         = time.Millisecond * 250
)

func setupClient(obj []client.Object) (client.Client, *runtime.Scheme, error) {

	s := scheme.Scheme
	if err := trainingv1alpha1.AddToScheme(s); err != nil {
		return nil, s, err
	}

	// create fake client
	cl := fake.NewClientBuilder().WithObjects(obj...).Build()

	return cl, s, nil

}

func generateRandomString() string {
	var seededRand *rand.Rand = rand.New(
		rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randStringLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func generateGithubIssueObject() *trainingv1alpha1.GithubIssue {
	name := generateRandomString()
	title := generateRandomString()
	description := generateRandomString()

	githubIssue := &trainingv1alpha1.GithubIssue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: trainingv1alpha1.GithubIssueSpec{
			Repo:        testRepo,
			Title:       title,
			Description: description,
		},
	}

	return githubIssue
}

func TestCloseIssueOnDelete(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(ginkgo.Fail)

	// create context and set environment variable
	ctx := context.Background()
	err := godotenv.Load("../config/manager/.env.secret")
	g.Expect(err).ToNot(HaveOccurred())

	// create githubissue object
	githubIssue := generateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := setupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s}

	// mock request to simulate reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      githubIssue.ObjectMeta.Name,
			Namespace: githubIssue.ObjectMeta.Namespace,
		},
	}
	res, err := r.Reconcile(ctx, req)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res).ToNot(BeNil())

	// get and delete the object
	githubIssueReconciled := trainingv1alpha1.GithubIssue{}
	err = cl.Get(ctx, req.NamespacedName, &githubIssueReconciled)
	g.Expect(err).ToNot(HaveOccurred())

	// delete issue using client and call reconcile again
	err = cl.Delete(ctx, &githubIssueReconciled)
	g.Expect(err).ToNot(HaveOccurred())

	res, err = r.Reconcile(ctx, req)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res).ToNot(BeNil())

	// get issue from github client
	ghClient := r.createGHClient(ctx)
	g.Expect(ghClient).ToNot(BeNil())

	owner, repo := r.extractOwnerRepoInfo(&githubIssueReconciled)
	title := githubIssueReconciled.Spec.Title

	issues, err := r.getIssuesInRepo(ctx, ghClient, owner, repo)
	g.Expect(err).ToNot(HaveOccurred())

	issue := r.getExistingIssue(issues, title)
	issueState := issue.GetState()

	g.Eventually(issueState, timeout, interval).Should(Equal("closed"))

}

func TestCreateIssueIfDoesntExist(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(ginkgo.Fail)

	// create context and set environment variable
	ctx := context.Background()
	err := godotenv.Load("../config/manager/.env.secret")
	g.Expect(err).ToNot(HaveOccurred())

	// create githubissue object
	githubIssue := generateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := setupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s}

	// mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      githubIssue.ObjectMeta.Name,
			Namespace: githubIssue.ObjectMeta.Namespace,
		},
	}
	res, err := r.Reconcile(ctx, req)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res).ToNot(BeNil())

	// check if the issue has been created by checking the conditions
	// of the status and make sure the issue is open
	githubIssueReconciled := trainingv1alpha1.GithubIssue{}
	err = cl.Get(ctx, req.NamespacedName, &githubIssueReconciled)
	g.Expect(err).ToNot(HaveOccurred())

	g.Eventually(apimeta.IsStatusConditionTrue(githubIssueReconciled.Status.Conditions, issueOpenConditionType), timeout, interval).Should(BeTrue())

}

func TestExtractOwnerRepoInfo(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(ginkgo.Fail)

	githubIssue := generateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := setupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s}

	owner, repo := r.extractOwnerRepoInfo(githubIssue)
	expectedOwner := "mzeevi"
	expectedRepo := "githubissues-operator"

	g.Expect(owner).To(Equal(expectedOwner))
	g.Expect(repo).To(Equal(expectedRepo))

}

func TestCreateGHClient(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(ginkgo.Fail)

	cl, s, err := setupClient([]client.Object{})
	g.Expect(err).ToNot(HaveOccurred())

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s}

	g.Expect(r.createGHClient(context.Background())).ToNot(BeNil())
}

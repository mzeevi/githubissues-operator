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
	"net/http"
	"testing"

	"github.com/google/go-github/v45/github"
	ghmock "github.com/migueleliasweb/go-github-mock/src/mock"
	trainingv1alpha1 "github.com/mzeevi/githubissues-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("GithubIssue controller", func() {
	Context("When updating GithubIssue objects", func() {
		name := "e2e-test-" + GenerateRandomString()
		title := "e2e-test-" + GenerateRandomString()
		description := "e2e-test-" + GenerateRandomString()
		repoName := "https://github.com/mzeevi/githubissues-operator"

		It("Should update the status of the object and its conditions", func() {
			By("Creating a new GithubIssue object")

			ctx := context.Background()

			githubIssue := &trainingv1alpha1.GithubIssue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: trainingv1alpha1.GithubIssueSpec{
					Repo:        repoName,
					Title:       title,
					Description: description,
				},
			}

			Expect(k8sClient.Create(ctx, githubIssue)).Should(Succeed())

			githubIssueLookupKey := types.NamespacedName{
				Name:      githubIssue.ObjectMeta.Name,
				Namespace: githubIssue.ObjectMeta.Namespace,
			}

			createdGithubIssue := trainingv1alpha1.GithubIssue{}
			k8sClient.Get(ctx, githubIssueLookupKey, &createdGithubIssue)
			// check the conditions of the object
			Eventually(func() bool {
				k8sClient.Get(ctx, githubIssueLookupKey, &createdGithubIssue)
				return apimeta.IsStatusConditionTrue(createdGithubIssue.Status.Conditions, issueOpenConditionType)
			}, timeout, interval).Should(BeTrue())

			By("Updating the decscription of an issue")
			newDescription := "updated-" + description
			createdGithubIssue.Spec.Description = newDescription
			Expect(k8sClient.Update(ctx, &createdGithubIssue)).Should(Succeed())

			updatedGithubIssue := trainingv1alpha1.GithubIssue{}
			Eventually(func() bool {
				k8sClient.Get(ctx, githubIssueLookupKey, &updatedGithubIssue)
				return updatedGithubIssue.Status.ActiveDescription == newDescription
			}, timeout, interval).Should(BeTrue())

			By("Closing an issue by deleting the object")
			Expect(k8sClient.Delete(ctx, &createdGithubIssue)).Should(Succeed())

			deletedGithubIssue := trainingv1alpha1.GithubIssue{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, githubIssueLookupKey, &deletedGithubIssue)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})

func TestFailedCreateIssue(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(Fail)

	// create context and set environment variable
	ctx := context.Background()

	// create githubissue object
	githubIssue := GenerateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := SetupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create mock githubissue client with mock data
	wantedError := "creating issue failed"
	mockedHTTPClient := ghmock.NewMockedHTTPClient(
		ghmock.WithRequestMatch(
			ghmock.GetReposIssuesByOwnerByRepo,
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String("Issue 1"),
					Body:  github.String("Issue 1 body"),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("closed"),
				},
			},
		),
		ghmock.WithRequestMatchHandler(
			ghmock.PostReposIssuesByOwnerByRepo,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ghmock.WriteError(
					w,
					http.StatusInternalServerError,
					wantedError,
				)
			}),
		),
	)

	ghClient := github.NewClient(mockedHTTPClient)

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s, ghClient}

	// mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      githubIssue.ObjectMeta.Name,
			Namespace: githubIssue.ObjectMeta.Namespace,
		},
	}
	_, err = r.Reconcile(ctx, req)
	ghErr, ok := err.(*github.ErrorResponse)

	g.Expect(ok).To(BeTrue())
	g.Expect(ghErr.Message).To(Equal(wantedError))
}

func TestFailedUpdateIssue(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(Fail)

	// create context and set environment variable
	ctx := context.Background()

	// create githubissue object
	githubIssue := GenerateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := SetupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create mock githubissue client with mock data
	wantedError := "updating description of issue failed"
	mockedHTTPClient := ghmock.NewMockedHTTPClient(
		ghmock.WithRequestMatch(
			ghmock.GetReposIssuesByOwnerByRepo,
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String(githubIssue.Spec.Title),
					Body:  github.String(githubIssue.Spec.Description),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("open"),
				},
			},
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String(githubIssue.Spec.Title),
					Body:  github.String(githubIssue.Spec.Description),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("open"),
				},
			},
		),
		ghmock.WithRequestMatchHandler(
			ghmock.PatchReposIssuesByOwnerByRepoByIssueNumber,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ghmock.WriteError(
					w,
					http.StatusInternalServerError,
					wantedError,
				)
			}),
		),
	)

	ghClient := github.NewClient(mockedHTTPClient)

	// create a GithubIssueReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s, ghClient}

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

	// call reconcile again for update to take place
	githubIssueReconciled := trainingv1alpha1.GithubIssue{}
	err = cl.Get(ctx, req.NamespacedName, &githubIssueReconciled)
	g.Expect(err).ToNot(HaveOccurred())

	githubIssueReconciled.Spec.Description = "updated description"
	err = cl.Update(ctx, &githubIssueReconciled)
	g.Expect(err).ToNot(HaveOccurred())

	_, err = r.Reconcile(ctx, req)
	ghErr, ok := err.(*github.ErrorResponse)

	g.Expect(ok).To(BeTrue())
	g.Expect(ghErr.Message).To(Equal(wantedError))

}

func TestCloseIssueOnDelete(t *testing.T) {
	g := NewGomegaWithT(t)
	RegisterFailHandler(Fail)

	// create context and set environment variable
	ctx := context.Background()

	// create githubissue object
	githubIssue := GenerateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := SetupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create mock githubissue client with mock data
	mockedHTTPClient := ghmock.NewMockedHTTPClient(
		ghmock.WithRequestMatch(
			ghmock.GetReposIssuesByOwnerByRepo,
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String(githubIssue.Spec.Title),
					Body:  github.String(githubIssue.Spec.Description),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("open"),
				},
			},
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String(githubIssue.Spec.Title),
					Body:  github.String(githubIssue.Spec.Description),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("open"),
				},
			},
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String(githubIssue.Spec.Title),
					Body:  github.String(githubIssue.Spec.Description),
					State: github.String("closed"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("open"),
				},
			},
		),
		ghmock.WithRequestMatch(
			ghmock.PatchReposIssuesByOwnerByRepoByIssueNumber,
			github.IssueRequest{
				Title: github.String(githubIssue.Spec.Title),
				Body:  github.String(githubIssue.Spec.Description),
			},
		),
	)

	ghClient := github.NewClient(mockedHTTPClient)

	// create a GithubIssueReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s, ghClient}

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
	RegisterFailHandler(Fail)

	// create context
	ctx := context.Background()

	// create githubissue object
	githubIssue := GenerateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := SetupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	// create mock githubissue client with mock data
	mockedHTTPClient := ghmock.NewMockedHTTPClient(
		ghmock.WithRequestMatch(
			ghmock.GetReposIssuesByOwnerByRepo,
			[]github.Issue{
				{
					ID:    github.Int64(123),
					Title: github.String("Issue 1"),
					Body:  github.String("Issue 1 body"),
					State: github.String("open"),
				},
				{
					ID:    github.Int64(456),
					Title: github.String("Issue 2"),
					Body:  github.String("Issue 2 body"),
					State: github.String("closed"),
				},
			},
		),
		ghmock.WithRequestMatch(
			ghmock.PostReposIssuesByOwnerByRepo,
			github.IssueRequest{
				Title: github.String(githubIssue.Spec.Title),
				Body:  github.String(githubIssue.Spec.Description),
			},
		),
	)

	ghClient := github.NewClient(mockedHTTPClient)

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s, ghClient}

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
	RegisterFailHandler(Fail)

	githubIssue := GenerateGithubIssueObject()

	obj := []client.Object{githubIssue}
	cl, s, err := SetupClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	ghClient := github.NewClient(&http.Client{})

	// create a NamespaceLabelReconciler object with the scheme and fake client
	r := &GithubIssueReconciler{cl, s, ghClient}

	owner, repo := r.extractOwnerRepoInfo(githubIssue)
	expectedOwner := testOwnerName
	expectedRepo := testRepoName

	g.Expect(owner).To(Equal(expectedOwner))
	g.Expect(repo).To(Equal(expectedRepo))

}

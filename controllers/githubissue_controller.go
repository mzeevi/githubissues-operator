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
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/google/go-github/v45/github"
	"golang.org/x/oauth2"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/mzeevi/githubissues-operator/api/v1alpha1"
)

// GithubIssueReconciler reconciles a GithubIssue object
type GithubIssueReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	ghIssueFinalizer string = "redhat.com/githubissue-finalizer"

	issueOpenConditionType   string = "IssueOpen"
	issueOpenConditionReason string = "IssueInOpenState"

	issueHasPRConditionType   string = "IssueHasPR"
	issueHasPRConditionReason string = "PullRequestExists"
)

//+kubebuilder:rbac:groups=training.redhat.com,resources=githubissues,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=training.redhat.com,resources=githubissues/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=training.redhat.com,resources=githubissues/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GithubIssue object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *GithubIssueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing GithubIssueReconciler")

	// fetch githubissue object
	var githubissue trainingv1alpha1.GithubIssue
	if err := r.Get(ctx, req.NamespacedName, &githubissue); err != nil {
		if errors.IsNotFound(err) {
			// request object not found, could have been deleted after reconcile request
			// return and don't requeue
			log.Info("GithubIssue resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// error reading the object - request
		log.Error(err, "unable to fetch githubissue")
		return ctrl.Result{}, err
	}

	// create github client and use personal access token to authenticate
	ghClient := r.createGHClient(ctx)

	// examine DeletionTimestamp to determine if object is under deletion
	if !githubissue.ObjectMeta.DeletionTimestamp.IsZero() {
		// handle finalizer deletion on object
		if err := r.deleteFinalizer(ctx, &githubissue, ghClient); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// the object is not being deleted, so if it does not have a finalizer,
	// then lets add the finalizer and update the object
	if err := r.addFinalizer(ctx, &githubissue, ghClient); err != nil {
		return ctrl.Result{}, nil
	}

	// pull information from request
	owner, repo := r.extractOwnerRepoInfo(&githubissue)
	title := githubissue.Spec.Title
	description := githubissue.Spec.Description

	// list all issues for the authenticated user
	issues, err := r.getIssuesInRepo(ctx, ghClient, owner, repo)
	if err != nil {
		log.Error(err, "unable to fetch issues from github repository", "owner", owner, "repo", repo)
		return ctrl.Result{}, err
	}

	// check if the title of the issue in the request exists in the list of issues in the repo
	// and act accordingly to either update the issue or create it
	issue := r.getExistingIssue(issues, title)

	if issue == nil {
		createdIssue, err := r.createNewIssue(ctx, ghClient, title, description, owner, repo)
		if err != nil {
			log.Error(err, "failed to create new issue on github repository", "owner", owner, "repo", repo)
			return ctrl.Result{}, err
		}
		issue = createdIssue
	}

	if issueBody := issue.GetBody(); issueBody != description {
		if err := r.updateIssueDescription(ctx, ghClient, issue, description, owner, repo); err != nil {
			log.Error(err, "failed to update issue on github repository", "owner", owner, "repo", repo, "issue", issue)
			return ctrl.Result{}, err
		}
	}

	// set conditions on issue
	log.Info("Setting conditions on object")
	r.setIssueOpenCondition(issue, &githubissue)
	r.setIssueHasPRCondition(issue, &githubissue)

	// update status
	log.Info("Updating githubissue status")
	if err := r.Status().Update(ctx, &githubissue); err != nil {
		log.Error(err, "unable to update githubissue status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// this function sets the condition of the issue that indicates
// whether the issue has pull requests
func (r *GithubIssueReconciler) setIssueHasPRCondition(issue *github.Issue, githubissue *trainingv1alpha1.GithubIssue) {
	hasPR := issue.GetPullRequestLinks()
	conditionStatus := metav1.ConditionTrue
	message := "The issue has a PR"

	if hasPR == nil {
		conditionStatus = metav1.ConditionFalse
		message = "The issue does not have a PR"

	}

	issueCondition := metav1.Condition{
		Type:    issueHasPRConditionType,
		Status:  conditionStatus,
		Reason:  issueHasPRConditionReason,
		Message: message,
	}

	apimeta.SetStatusCondition(&githubissue.Status.Conditions, issueCondition)
}

// this function sets the condition of the issue that indicates
// whether the issue is currently in open state
func (r *GithubIssueReconciler) setIssueOpenCondition(issue *github.Issue, githubissue *trainingv1alpha1.GithubIssue) {
	issueState := issue.GetState()
	conditionStatus := metav1.ConditionTrue
	message := "The issue is in open state"

	if issueState == "closed" {
		conditionStatus = metav1.ConditionFalse
		message = "The issue is in open state"
	}

	issueCondition := metav1.Condition{
		Type:    issueOpenConditionType,
		Status:  conditionStatus,
		Reason:  issueOpenConditionReason,
		Message: message,
	}

	apimeta.SetStatusCondition(&githubissue.Status.Conditions, issueCondition)
}

// this function handles the deletion of a finalizer to an object
func (r *GithubIssueReconciler) deleteFinalizer(ctx context.Context, githubissue *trainingv1alpha1.GithubIssue, ghClient *github.Client) error {
	log := log.FromContext(ctx)
	log.Info("Handling finalizer deletion")

	if controllerutil.ContainsFinalizer(githubissue, ghIssueFinalizer) {
		owner, repo := r.extractOwnerRepoInfo(githubissue)
		issues, err := r.getIssuesInRepo(ctx, ghClient, owner, repo)
		if err != nil {
			log.Error(err, "unable to fetch issues from github repository", "owner", owner, "repo", repo)
			return err
		}

		title := githubissue.Spec.Title
		issue := r.getExistingIssue(issues, title)

		if issue != nil {
			issueNumber := issue.GetNumber()

			if err := r.closeIssue(ctx, ghClient, issueNumber, owner, repo); err != nil {
				log.Error(err, "failed to lock issue", "owner", owner, "repo", repo, "issue", issue)
				return err
			}

			controllerutil.RemoveFinalizer(githubissue, ghIssueFinalizer)
			if err := r.Update(ctx, githubissue); err != nil {
				log.Error(err, "failed to update githubissue")
				return err
			}
		}
	}
	return nil
}

// this function handles the addition of a finalizer to an object
func (r *GithubIssueReconciler) addFinalizer(ctx context.Context, githubissue *trainingv1alpha1.GithubIssue, ghClient *github.Client) error {
	log := log.FromContext(ctx)
	log.Info("Handling finalizer addition")

	if !controllerutil.ContainsFinalizer(githubissue, ghIssueFinalizer) {
		controllerutil.AddFinalizer(githubissue, ghIssueFinalizer)
		if err := r.Update(ctx, githubissue); err != nil {
			log.Error(err, "failed to update namespaceLabel")
			return err
		}
	}

	return nil
}

// this function changes the state of an issue to closed
// IssueRequest is initiated with what needs to be updated and
// not setting a value for a parameter means keeping the current parameters the same
func (r *GithubIssueReconciler) closeIssue(ctx context.Context, ghClient *github.Client, issueNumber int, owner, repo string) error {
	log := log.FromContext(ctx)

	closedState := "closed"
	issueRequest := github.IssueRequest{
		State: &closedState,
	}

	_, response, err := ghClient.Issues.Edit(ctx, owner, repo, issueNumber, &issueRequest)

	if err != nil {
		log.Error(err, "unable to close issue")
		return err
	}

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status code: %d", response.StatusCode)
		return err
	}

	return nil
}

// this function creates a new issue
// IssueRequest is initiated with what needs to be updated and
// not setting a value for a parameter means keeping the current parameters the same
func (r *GithubIssueReconciler) createNewIssue(ctx context.Context, ghClient *github.Client, title, description, owner, repo string) (*github.Issue, error) {
	log := log.FromContext(ctx)

	issueRequest := github.IssueRequest{
		Title: &title,
		Body:  &description,
	}

	issue, response, err := ghClient.Issues.Create(ctx, owner, repo, &issueRequest)

	if err != nil {
		log.Error(err, "unable to create issue")
		return issue, err
	}

	if response.StatusCode != http.StatusCreated {
		err := fmt.Errorf("unexpected status code: %d", response.StatusCode)
		return issue, err
	}

	return issue, nil

}

// this function updates the description of an issue
// IssueRequest is initiated with what needs to be updated and
// not setting a value for a parameter means keeping the current parameters the same
func (r *GithubIssueReconciler) updateIssueDescription(ctx context.Context, ghClient *github.Client, issue *github.Issue, description, owner, repo string) error {
	log := log.FromContext(ctx)

	issueRequest := github.IssueRequest{
		Body: &description,
	}

	issueNumber := issue.GetNumber()
	_, response, err := ghClient.Issues.Edit(ctx, owner, repo, issueNumber, &issueRequest)

	if err != nil {
		log.Error(err, "unable to update issue description")
		return err
	}

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status code: %d", response.StatusCode)
		return err
	}

	return nil
}

// this function checks whether a title of an issue exists in the current open issues
// in a repository and returns the issue if it exsists and nil otherwise
func (r *GithubIssueReconciler) getExistingIssue(issues []*github.Issue, title string) *github.Issue {
	for _, issue := range issues {
		if issueTitle := issue.GetTitle(); issueTitle == title {
			return issue
		}
	}
	return nil
}

// this function returns the issues in a repository
// and an error if there is a problem with fetching the issues
// a problem may be in the status code (i.e. 403 Status Code) or general
func (r *GithubIssueReconciler) getIssuesInRepo(ctx context.Context, ghClient *github.Client, owner, repo string) ([]*github.Issue, error) {
	log := log.FromContext(ctx)
	issues, response, err := ghClient.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{})

	if err != nil {
		log.Error(err, "unable to fetch issues from github")
		return issues, err
	}

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status code: %d", response.StatusCode)
		return issues, err
	}

	return issues, nil
}

// this function takes a GithubIssue object and extracts
// the owner and repo information from the repository URL in the spec
func (r *GithubIssueReconciler) extractOwnerRepoInfo(githubissue *trainingv1alpha1.GithubIssue) (string, string) {
	repositoryURL := githubissue.Spec.Repo
	re := regexp.MustCompile(`([^\/]+)\/([^\/]+)$`)

	ownerRepo := re.FindString(repositoryURL)
	ownerRepoSlice := strings.Split(ownerRepo, "/")

	owner := ownerRepoSlice[0]
	repo := ownerRepoSlice[1]

	return owner, repo
}

// this function uses the personal access token to authenticate
// and returns a github client to use in reconcile
func (r *GithubIssueReconciler) createGHClient(ctx context.Context) *github.Client {
	ghPersonalAccessToken := os.Getenv("GH_PERSONAL_TOKEN")
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: ghPersonalAccessToken},
	)

	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)

	return ghClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubIssueReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trainingv1alpha1.GithubIssue{}).
		Complete(r)
}

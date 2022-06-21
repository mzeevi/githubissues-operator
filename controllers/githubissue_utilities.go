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
	"os"
	"time"

	"github.com/google/go-github/v45/github"
	trainingv1alpha1 "github.com/mzeevi/githubissues-operator/api/v1alpha1"
	"golang.org/x/oauth2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testRepoName     = "testRepo"
	testOwnerName    = "testOrg"
	testRepo         = "https://github.com/" + testOwnerName + "/" + testRepoName
	testNamespace    = "default"
	charset          = "abcdefghijklmnopqrstuvwxyz" + "0123456789"
	randStringLength = 10
	timeout          = time.Second * 10
	duration         = time.Second * 10
	interval         = time.Millisecond * 250
)

func GetGithubClient(ctx context.Context) *github.Client {
	ghPersonalAccessToken := os.Getenv("GH_PERSONAL_TOKEN")
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: ghPersonalAccessToken},
	)

	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)

	return ghClient
}

func SetupClient(obj []client.Object) (client.Client, *runtime.Scheme, error) {

	s := scheme.Scheme
	if err := trainingv1alpha1.AddToScheme(s); err != nil {
		return nil, s, err
	}

	// create fake client
	cl := fake.NewClientBuilder().WithObjects(obj...).Build()

	return cl, s, nil

}

func GenerateRandomString() string {
	var seededRand *rand.Rand = rand.New(
		rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randStringLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func GenerateGithubIssueObject() *trainingv1alpha1.GithubIssue {
	name := GenerateRandomString()
	title := GenerateRandomString()
	description := GenerateRandomString()

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

package main

import (
	"encoding/base64"
	"fmt"
	"k8s.io/apimachinery/pkg/util/sets"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"
	"time"

	"github.com/opensourceways/community-robot-lib/config"
	framework "github.com/opensourceways/community-robot-lib/robot-gitee-framework"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"github.com/sirupsen/logrus"
)

const botName = "sig-guide"

const (
	forIssueReply = `Hi ***@%s***, 
if you want to get quick review about your issue, please contact the owner in first: @%s ,
and then any of the maintainers: @%s
and then any of the committers: @%s
if you have any question, please contact the SIG:%s.`

	forPRReply = `Hi ***@%s***, 
if you want to get quick review about your pull request, please contact the owner in first: @%s ,
and then any of the maintainers: @%s
and then any of the committers: @%s
if you have any question, please contact the SIG:%s.`

	sigLink = `[%s](%s)`

	notice = `Hi ***@%s***, please use the command "/sig xxx" to add a SIG label to this issue.`
)

var (
	sigLabelRegex = regexp.MustCompile(`(?m)^/sig\s*(.*?)\s*$`)
)

type iClient interface {
	CreatePRComment(owner, repo string, number int32, comment string) error
	CreateIssueComment(owner, repo string, number string, comment string) error
	GetBot() (sdk.User, error)
	GetIssueLabels(org, repo, number string) ([]sdk.Label, error)
	GetPullRequestChanges(org, repo string, number int32) ([]sdk.PullRequestFiles, error)
	AddMultiPRLabel(org, repo string, number int32, label []string) error
	GetPathContent(org, repo, path, ref string) (sdk.Content, error)
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type robot struct {
	cli iClient
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}
	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(p framework.HandlerRegitster) {
	p.RegisterIssueHandler(bot.handleIssueEvent)
	p.RegisterPullRequestHandler(bot.handlePREvent)
	p.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handleIssueEvent(e *sdk.IssueEvent, c config.Config, log *logrus.Entry) error {
	if e.GetAction() != sdk.ActionOpen {
		return nil
	}

	org, repo := e.GetOrgRepo()
	author := e.GetIssueAuthor()
	number := e.GetIssueNumber()

	return bot.cli.CreateIssueComment(org, repo, number, fmt.Sprintf(notice, author))
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, c config.Config, log *logrus.Entry) error {
	// when pr has been opened, add sig label to it.
	if sdk.GetPullRequestAction(e) == sdk.ActionOpen {
		org, repo := e.GetOrgRepo()
		number := e.GetPRNumber()
		label, err := bot.genSigLabel(org, repo, number)
		if err != nil || label == "" {
			return err
		}

		return bot.cli.AddMultiPRLabel(org, repo, number, []string{label})
	}

	// when pr's label has been changed
	if sdk.GetPullRequestAction(e) != sdk.PRActionUpdatedLabel {
		return nil
	}

	org, repo := e.GetOrgRepo()
	author := e.GetPRAuthor()
	number := e.GetPRNumber()
	msgs := make([]string, 0)

	staleLabels := sets.NewString()
	for _, label := range e.GetPullRequest().StaleLabels {
		staleLabels.Insert(label.Name)
	}
	diffLabels := sets.NewString()
	if v := e.GetPRLabelSet().Difference(staleLabels); len(v) > 0 {
		for l := range v {
			diffLabels.Insert(l)
		}
	}

	copyDiffLabels := diffLabels
	for d := range copyDiffLabels {
		if !strings.HasPrefix(d, "sig/") {
			diffLabels.Delete(d)
		}
	}

	if len(diffLabels) == 0 {
		return nil
	}

	// get pr changed files
	changes, err := bot.cli.GetPullRequestChanges(org, repo, number)
	if err != nil {
		return err
	}

	for _, f := range changes {
		if len(msgs) > 0 {
			break
		}
		msg, err := bot.genSpecialWelcomeMessage(repo, author, f.Filename, diffLabels)
		if err != nil {
			return err
		}

		msgs = append(msgs, msg)
	}

	comment := strings.Join(msgs, "")
	if len(comment) == 0 {
		return nil
	}

	return bot.cli.CreatePRComment(org, repo, e.GetPRNumber(), comment)
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, c config.Config, log *logrus.Entry) error {
	if !e.IsCreatingCommentEvent() {
		return nil
	}

	if e.IsPullRequest() {
		return nil
	}

	comment := e.GetComment().GetBody()
	if !sigLabelRegex.MatchString(comment) {
		return nil
	}

	org, repo := e.GetOrgRepo()
	number := e.GetIssueNumber()
	author := e.GetIssueAuthor()

	time.Sleep(100 * time.Millisecond)
	labels, err := bot.cli.GetIssueLabels(org, repo, number)
	if err != nil {
		return err
	}

	sigNames := make(map[string]string, 0)
	// var repositories []RepoMember
	repositories := make(map[string][]RepoMember, 0)
	deOwners := sets.NewString()
	for _, l := range labels {
		if len(sigNames) > 0 {
			break
		}

		if strings.HasPrefix(l.Name, "sig/") {
			sigs, err := bot.decodeSigsContent()
			if err != nil {
				return err
			}

			for _, d := range sigs.DefaultOwners {
				deOwners.Insert(d.GiteeID)
			}

			for _, sig := range sigs.Sigs {
				if l.Name == sig.SigLabel {
					sigNames[sig.Name] = sig.SigLink
					repositories[sig.Name] = sig.Repos
				}
			}
		}
	}

	// firstly @ who to resolve this problem
	owner := make([]string, 0)
	for _, r := range repositories {
		for _, rp := range r {
			for _, rps := range rp.Repo {
				if repo == rps {
					for _, o := range rp.Owner {
						owner = append(owner, o.GiteeID)
					}
				}
			}
		}
	}

	if len(owner) == 0 {
		owner = append(owner, deOwners.UnsortedList()...)
	}

	// secondly @ persons to resolve
	maintainers := make([]string, 0)
	committers := make([]string, 0)
	for k := range sigNames {
		os, cs, err := bot.decodeOWNERSContent(k)
		if err != nil {
			return err
		}

		maintainers = append(maintainers, os...)
		committers = append(committers, cs...)
	}

	// remove duplicate
	for _, o := range owner {
		for i, j := range maintainers {
			if o == j {
				maintainers = append(maintainers[:i], maintainers[:i+1]...)
			}
		}
		for m, n := range committers {
			if o == n {
				committers = append(committers[:m], committers[:m+1]...)
			}
		}
	}

	// gen hyper link in messages
	sigsLinks := make([]string, 0)
	for k, v := range sigNames {
		sigsLinks = append(sigsLinks, fmt.Sprintf(sigLink, k, v))
	}

	message := fmt.Sprintf(forIssueReply, author, strings.Join(owner, " , @"),
		strings.Join(maintainers, " , @"), strings.Join(committers, " , @"),
		strings.Join(sigsLinks, ""))

	return bot.cli.CreateIssueComment(org, repo, number, message)
}

func (bot *robot) genSpecialWelcomeMessage(repo, author, fileName string, labels sets.String) (string, error) {
	owners := sets.NewString()
	sigName := make(map[string]string, 0)
	deOwners := sets.NewString()
	diffHasSigLabel := false
	for l := range labels {
		if len(owners) > 0 {
			break
		}

		if strings.HasPrefix(l, "sig/") {
			diffHasSigLabel = true
			fileOwner, defaultOwners, sig, link, err := bot.getFileOwner(l, fmt.Sprintf("%s/%s", repo, fileName), repo)
			if err != nil {
				return "", err
			}

			owners.Insert(fileOwner.UnsortedList()...)
			deOwners.Insert(defaultOwners.UnsortedList()...)
			sigName[sig] = link
		}
	}

	if len(owners) == 0 {
		owners.Insert(deOwners.UnsortedList()...)
	}

	maintainers := make([]string, 0)
	committers := make([]string, 0)
	for sn := range sigName {
		os, cs, err := bot.decodeOWNERSContent(sn)
		if err != nil {
			return "", err
		}

		maintainers = append(maintainers, os...)
		committers = append(committers, cs...)
	}

	// remove duplicate
	for o := range owners {
		for i, j := range maintainers {
			if o == j {
				maintainers = append(maintainers[:i], maintainers[:i+1]...)
			}
		}
		for m, n := range committers {
			if o == n {
				committers = append(committers[:m], committers[:m+1]...)
			}
		}
	}

	// gen hyper link in messages
	sigsLinks := make([]string, 0)
	for k, v := range sigName {
		sigsLinks = append(sigsLinks, fmt.Sprintf(sigLink, k, v))
	}

	if !diffHasSigLabel {
		return "", nil
	}

	return fmt.Sprintf(forPRReply, author, strings.Join(owners.UnsortedList(), " ,@"),
		strings.Join(maintainers, " , @"),
		strings.Join(committers, " , @"),
		strings.Join(sigsLinks, "")), nil
}

func (bot *robot) getFileOwner(label, fileName, repo string) (sets.String, sets.String, string, string, error) {
	sigs, err := bot.decodeSigsContent()
	if err != nil {
		return nil, nil, "", "", err
	}

	sigName := ""
	link := ""

	defaultOwners := sets.NewString()
	for _, d := range sigs.DefaultOwners {
		defaultOwners.Insert(d.GiteeID)
	}

	var sig Sig
	for _, s := range sigs.Sigs {
		if label == s.SigLabel {
			sig = s
			sigName = s.Name
			link = s.SigLink
		}
	}

	first := sets.NewString()
	for _, s := range sig.Files {
		if len(first) > 0 {
			break
		}
		for _, ff := range s.File {
			if ff == fileName {
				for _, o := range s.Owner {
					first.Insert(o.GiteeID)
				}
			}
		}
	}

	if len(first) == 0 {
		for _, r := range sig.Repos {
			for _, rp := range r.Repo {
				if rp == repo {
					for _, o := range r.Owner {
						first.Insert(o.GiteeID)
					}
				}
			}
		}
	}

	return first, defaultOwners, sigName, link, nil
}

func (bot *robot) decodeSigsContent() (*SigYaml, error) {
	fileContent, err := bot.cli.GetPathContent("opengauss", "tc", "gauss_relationship.yaml", "master")
	if err != nil {
		return nil, err
	}

	c, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return nil, err
	}

	var sigs SigYaml
	err = yaml.Unmarshal(c, &sigs)
	if err != nil {
		return nil, err
	}

	return &sigs, nil
}

func (bot *robot) decodeOWNERSContent(sigName string) ([]string, []string, error) {
	fileContent, err := bot.cli.GetPathContent("opengauss", "tc", fmt.Sprintf("sigs/%s/OWNERS", sigName), "master")
	if err != nil {
		return nil, nil, err
	}

	c, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return nil, nil, err
	}

	type OWNERS struct {
		Maintainers []string `json:"maintainers,omitempty"`
		Committers  []string `json:"committers,omitempty"`
	}

	var o OWNERS
	err = yaml.Unmarshal(c, &o)
	if err != nil {
		return nil, nil, err
	}

	owner := o.Maintainers
	committer := o.Committers

	return owner, committer, nil
}

func (bot *robot) genSigLabel(org, repo string, number int32) (string, error) {
	changes, err := bot.cli.GetPullRequestChanges(org, repo, number)
	if err != nil {
		return "", err
	}

	sigs, err := bot.decodeSigsContent()
	if err != nil {
		return "", err
	}

	sigLabel := ""
	for _, c := range changes {
		if sigLabel != "" {
			break
		}
		for _, s := range sigs.Sigs {
			for _, f := range s.Files {
				for _, ff := range f.File {
					if fmt.Sprintf("%s/%s", repo, c.Filename) == ff {
						sigLabel = s.SigLabel
					}
				}
			}
		}
	}

	if sigLabel != "" {
		return sigLabel, nil
	}

	for _, s := range sigs.Sigs {
		if sigLabel != "" {
			break
		}

		for _, r := range s.Repos {
			for _, rr := range r.Repo {
				if repo == rr {
					sigLabel = s.SigLabel
				}
			}
		}
	}

	return sigLabel, nil
}

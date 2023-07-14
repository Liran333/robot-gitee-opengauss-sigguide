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
if you have any question, please contact the SIG: %s.`

	forPRReply = `Hi ***@%s***, 
if you want to get quick review about your pull request, please contact the owner in first: @%s ,
and then any of the maintainers: @%s
and then any of the committers: @%s
if you have any question, please contact the SIG: %s.`

	sigLink = `[%s](%s)`

	notice = `Hi ***@%s***, please use the command ***/sig xxx*** to add a SIG label to this issue.
For example: ***/sig sqlengine*** or ***/sig storageengine*** or ***/sig om*** or ***/sig ai*** and so on.
You can find more SIG labels from [Here](https://opengauss.org/zh/member.html#sig).
If you have no idea about that, please contact with @%s .`
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
	AddMultiIssueLabel(org, repo, number string, label []string) error
	RemovePRLabels(org, repo string, number int32, labels []string) error
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

	bc, _ := bot.getConfig(c, org, repo)

	if repo == "openGauss-server" {
		_, _, _, _, deOwners, err := bot.genIssueSigLabel(repo)
		if err != nil {
			return err
		}
		return bot.cli.CreateIssueComment(org, repo, number, fmt.Sprintf(notice, e.GetIssueAuthor(),
			strings.Join(deOwners.UnsortedList(), " , @")))
	}

	label, sig, link, firstOwners, deOwners, err := bot.genIssueSigLabel(repo)
	if err != nil {
		return err
	}

	if label == "" || sig == "" || link == "" {
		return nil
	}

	time.Sleep(600 * time.Millisecond)
	err = bot.cli.AddMultiIssueLabel(org, repo, number, []string{label})
	if err != nil {
		return err
	}

	maintainers, committers := sets.NewString(), sets.NewString()
	if bc.CustomizeMembers {
		ms, cs, err := bot.decodeSpecialOWNERSContent(sig, org, repo)
		if err != nil {
			return err
		}
		maintainers.Insert(ms...)
		committers.Insert(cs...)
	} else {
		ms, cs, err := bot.decodeOWNERSContent(sig)
		if err != nil {
			return err
		}
		maintainers.Insert(ms...)
		committers.Insert(cs...)
	}

	if len(firstOwners) == 0 {
		firstOwners.Insert(deOwners.UnsortedList()...)
	}

	//remove duplicate
	//for f := range firstOwners {
	//	for m := range maintainers {
	//		if m == f {
	//			maintainers.Delete(m)
	//		}
	//	}
	//
	//	for c := range committers {
	//		if c == f {
	//			committers.Delete(c)
	//		}
	//	}
	//}

	message := fmt.Sprintf(forIssueReply, author, strings.Join(firstOwners.UnsortedList(), " , @"),
		strings.Join(maintainers.UnsortedList(), " , @"), strings.Join(committers.UnsortedList(), " , @"),
		fmt.Sprintf(sigLink, sig, link))

	return bot.cli.CreateIssueComment(org, repo, number, message)
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

		time.Sleep(700 * time.Millisecond)

		return bot.cli.AddMultiPRLabel(org, repo, number, []string{label})
	}

	if sdk.GetPullRequestAction(e) == sdk.PRActionChangedSourceBranch {
		return bot.dealPRPush(e)
	}

	// when pr's label has been changed
	if sdk.GetPullRequestAction(e) != sdk.PRActionUpdatedLabel {
		return nil
	}

	org, repo := e.GetOrgRepo()
	author := e.GetPRAuthor()
	number := e.GetPRNumber()
	msgs := make([]string, 0)

	bc, _ := bot.getConfig(c, org, repo)

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
		msg, err := bot.genSpecialWelcomeMessage(bc, org, repo, author, f.Filename, diffLabels)
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

	if e.IsIssue() {
		org, repo := e.GetOrgRepo()
		bc, _ := bot.getConfig(c, org, repo)
		err := bot.dealIssueNote(e, bc)
		if err != nil {
			return err
		}
	}

	return nil
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

	var o OWNERS
	err = yaml.Unmarshal(c, &o)
	if err != nil {
		return nil, nil, err
	}

	owner := o.Maintainers
	committer := o.Committers

	return owner, committer, nil
}

func (bot *robot) decodeSpecialOWNERSContent(sigName, org, repo string) ([]string, []string, error) {
	fileContent, err := bot.cli.GetPathContent("opengauss", "tc", fmt.Sprintf("sigs/%s/OWNERS", sigName), "master")
	if err != nil {
		return nil, nil, err
	}

	c, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return nil, nil, err
	}

	var o SpecialOWNERS
	err = yaml.Unmarshal(c, &o)
	if err != nil {
		return nil, nil, err
	}

	var owner []string
	var committer []string
	p := fmt.Sprintf("%s/%s", org, repo)
	for _, v := range o.Repositories {
		for _, k := range v.Repo {
			if k == p {
				owner = v.Maintainers
			}
		}
	}

	for _, v := range o.Repositories {
		for _, k := range v.Repo {
			if k == p {
				committer = v.Committers
			}
		}
	}

	return owner, committer, nil
}

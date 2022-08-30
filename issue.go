package main

import (
	"fmt"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
	"strings"
	"time"
)

func (bot *robot) dealIssueNote(e *sdk.NoteEvent) error {
	comment := e.GetComment().GetBody()
	if !sigLabelRegex.MatchString(comment) {
		return nil
	}

	org, repo := e.GetOrgRepo()
	number := e.GetIssueNumber()
	author := e.GetIssueAuthor()

	// all repos' issues can be changed labels by their authors
	//if repo != "openGauss-server" {
	//	return nil
	//}

	// time.Sleep(400 * time.Millisecond)
	//labels, err := bot.cli.GetIssueLabels(org, repo, number)
	//if err != nil {
	//	return err
	//}

	sigNames := make(map[string]string, 0)
	sigLabel := fmt.Sprintf("sig/%s", strings.Split(comment, " ")[1])
	repositories := make(map[string][]RepoMember, 0)
	deOwners := sets.NewString()
	//for _, l := range labels {
	//	if len(sigNames) > 0 {
	//		break
	//	}
	//
	//	if strings.HasPrefix(l.Name, "sig/") {
	//		sigs, err := bot.decodeSigsContent()
	//		if err != nil {
	//			return err
	//		}
	//
	//		for _, d := range sigs.DefaultOwners {
	//			deOwners.Insert(d.GiteeID)
	//		}
	//
	//		for _, sig := range sigs.Sigs {
	//			if l.Name == sig.SigLabel {
	//				sigNames[sig.Name] = sig.SigLink
	//				repositories[sig.Name] = sig.Repos
	//			}
	//		}
	//	}
	//}

	sigs, err := bot.decodeSigsContent()
	if err != nil {
		return err
	}

	for _, d := range sigs.DefaultOwners {
		deOwners.Insert(d.GiteeID)
	}

	for _, sig := range sigs.Sigs {
		if sigLabel == sig.SigLabel {
			sigNames[sig.Name] = sig.SigLink
			repositories[sig.Name] = sig.Repos
		}
	}

	// firstly @ who to resolve this problem
	owner := sets.NewString()
	for _, r := range repositories {
		for _, rp := range r {
			for _, rps := range rp.Repo {
				if repo == rps {
					for _, o := range rp.Owner {
						owner.Insert(o.GiteeID)
					}
				}
			}
		}
	}

	if len(owner) == 0 {
		owner.Insert(deOwners.UnsortedList()...)
	}

	maintainers := sets.NewString()
	committers := sets.NewString()
	for sn := range sigNames {
		os, cs, err := bot.decodeOWNERSContent(sn)
		if err != nil {
			return err
		}

		maintainers.Insert(os...)
		committers.Insert(cs...)
	}

	// remove duplicate
	//for o := range owner {
	//	for j := range maintainers {
	//		if o == j {
	//			maintainers.Delete(j)
	//		}
	//	}
	//	for n := range committers {
	//		if o == n {
	//			committers.Delete(n)
	//		}
	//	}
	//}

	// gen hyper link in messages
	sigsLinks := make([]string, 0)
	for k, v := range sigNames {
		sigsLinks = append(sigsLinks, fmt.Sprintf(sigLink, k, v))
	}

	if len(maintainers) == 0 || len(committers) == 0 || len(sigsLinks) == 0 {
		return nil
	}

	message := fmt.Sprintf(forIssueReply, author, strings.Join(owner.UnsortedList(), " , @"),
		strings.Join(maintainers.UnsortedList(), " , @"), strings.Join(committers.UnsortedList(), " , @"),
		strings.Join(sigsLinks, ""))

	time.Sleep(500 * time.Millisecond)
	return bot.cli.CreateIssueComment(org, repo, number, message)
}

func (bot *robot) genIssueSigLabel(repo string) (string, string, string, sets.String, sets.String, error) {
	sigs, err := bot.decodeSigsContent()
	if err != nil {
		return "", "", "", nil, nil, err
	}

	sigLabel := ""
	sig := ""
	sigLinks := ""
	firstOwners := sets.NewString()
	defaultOwners := sets.NewString()

	for _, d := range sigs.DefaultOwners {
		defaultOwners.Insert(d.GiteeID)
	}

	for _, s := range sigs.Sigs {
		if sigLabel != "" {
			break
		}

		for _, ss := range s.Repos {
			for _, r := range ss.Repo {
				if r == repo {
					sigLabel = s.SigLabel
					sig = s.Name
					sigLinks = s.SigLink
					for _, o := range ss.Owner {
						firstOwners.Insert(o.GiteeID)
					}
				}
			}
		}
	}

	if sigLabel != "" && sig != "" && sigLinks != "" {
		return sigLabel, sig, sigLinks, firstOwners, defaultOwners, nil
	}

	return "", "", "", nil, nil, err
}

package main

import (
	"fmt"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
	"strings"
)

//func (bot *robot) dealPRNote(e *sdk.NoteEvent) error {
//	comment := e.GetComment().GetBody()
//	if !sigLabelRegex.MatchString(comment) {
//		return nil
//	}
//
//	org, repo := e.GetOrgRepo()
//	number := e.GetPRNumber()
//	author := e.GetPRAuthor()
//
//	sigNames := make(map[string]string, 0)
//	sig := ""
//	sigLabel := fmt.Sprintf("sig/%s", strings.Split(comment, " ")[1])
//
//	changes, err := bot.cli.GetPullRequestChanges(org, repo, number)
//	if err != nil {
//		return err
//	}
//
//	deOwners := sets.NewString()
//	sigs, err := bot.decodeSigsContent()
//	if err != nil {
//		return err
//	}
//
//	for _, d := range sigs.DefaultOwners {
//		deOwners.Insert(d.GiteeID)
//	}
//
//	firstContactOwners := sets.NewString()
//	repos := make(map[string][]RepoMember, 0)
//	files := make(map[string][]FileMember, 0)
//	for _, s := range sigs.Sigs {
//		if s.SigLabel == sigLabel {
//			sigNames[s.Name] = s.SigLink
//			files[s.Name] = s.Files
//			repos[s.Name] = s.Repos
//			sig = s.Name
//		}
//	}
//
//	for _, c := range changes {
//		for _, f := range files {
//			for _, ff := range f {
//				for _, fff := range ff.File {
//					if c.Filename == fff {
//						for _, o := range ff.Owner {
//							firstContactOwners.Insert(o.GiteeID)
//						}
//					}
//				}
//			}
//		}
//	}
//
//	if len(firstContactOwners) == 0 {
//		for _, r := range repos {
//			for _, rp := range r {
//				for _, rps := range rp.Repo {
//					if repo == rps {
//						for _, o := range rp.Owner {
//							firstContactOwners.Insert(o.GiteeID)
//						}
//					}
//				}
//			}
//		}
//	}
//
//	if len(firstContactOwners) == 0 {
//		firstContactOwners.Insert(deOwners.UnsortedList()...)
//	}
//
//	maintainers := sets.NewString()
//	committers := sets.NewString()
//	ms, cs, err := bot.decodeOWNERSContent(sig)
//	maintainers.Insert(ms...)
//	committers.Insert(cs...)
//
//	// gen hyper link in messages
//	sigsLinks := make([]string, 0)
//	for k, v := range sigNames {
//		sigsLinks = append(sigsLinks, fmt.Sprintf(sigLink, k, v))
//	}
//
//	message := fmt.Sprintf(forPRReply, author, strings.Join(firstContactOwners.UnsortedList(), " , @"),
//		strings.Join(maintainers.UnsortedList(), " , @"), strings.Join(committers.UnsortedList(), " , @"),
//		strings.Join(sigsLinks, ""))
//
//	return bot.cli.CreatePRComment(org, repo, e.GetPRNumber(), message)
//}

func (bot *robot) genSpecialWelcomeMessage(bc *botConfig, org, repo, author, fileName string, labels sets.String) (string, error) {
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

	maintainers := sets.NewString()
	committers := sets.NewString()
	for sn := range sigName {
		if bc.CustomizeMembers {
			os, cs, err := bot.decodeSpecialOWNERSContent(sn, org, repo)
			if err != nil {
				return "", err
			}

			maintainers.Insert(os...)
			committers.Insert(cs...)
			continue
		}

		os, cs, err := bot.decodeOWNERSContent(sn)
		if err != nil {
			return "", err
		}

		maintainers.Insert(os...)
		committers.Insert(cs...)
	}

	// remove duplicate
	//for o := range owners {
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
	for k, v := range sigName {
		sigsLinks = append(sigsLinks, fmt.Sprintf(sigLink, k, v))
	}

	if !diffHasSigLabel {
		return "", nil
	}

	return fmt.Sprintf(forPRReply, author, strings.Join(owners.UnsortedList(), " ,@"),
		strings.Join(maintainers.UnsortedList(), " , @"),
		strings.Join(committers.UnsortedList(), " , @"),
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

func (bot *robot) dealPRPush(e *sdk.PullRequestEvent) error {
	org, repo := e.GetOrgRepo()
	num := e.GetPRNumber()

	labels := e.GetPRLabelSet()
	currentLabel := sets.NewString()
	for l := range labels {
		if strings.HasPrefix(l, "sig/") {
			currentLabel.Insert(l)
		}
	}

	changes, err := bot.cli.GetPullRequestChanges(org, repo, num)
	if err != nil {
		return err
	}

	label := ""

	sigs, err := bot.decodeSigsContent()
	if err != nil {
		return err
	}

	for _, s := range sigs.Sigs {
		if label != "" {
			break
		}

		for _, f := range s.Files {
			for _, ff := range f.File {
				for _, c := range changes {
					if fmt.Sprintf("%s/%s", repo, c.Filename) == ff {
						label = s.SigLabel
						if _, ok := currentLabel[label]; ok {
							label = ""
						}
					}
				}
			}
		}
	}

	if label != "" {
		err := bot.cli.RemovePRLabels(org, repo, num, currentLabel.UnsortedList())
		if err != nil {
			return err
		}

		return bot.cli.AddMultiPRLabel(org, repo, num, []string{label})
	}

	return nil
}

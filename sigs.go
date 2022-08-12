package main

type SigYaml struct {
	Sigs          []Sig    `json:"sigs,omitempty"`
	DefaultOwners []Member `json:"default_owners,omitempty"`
}

type Sig struct {
	Name     string       `json:"name,omitempty"`
	SigLabel string       `json:"sig_label,omitempty"`
	SigLink  string       `json:"sig_link,omitempty"`
	Files    []FileMember `json:"files,omitempty"`
	Repos    []RepoMember `json:"repos,omitempty"`
}

type FileMember struct {
	File  []string `json:"file,omitempty"`
	Owner []Member `json:"owner,omitempty"`
}

type RepoMember struct {
	Repo  []string `json:"repo,omitempty"`
	Owner []Member `json:"owner,omitempty"`
}

type Member struct {
	GiteeID      string `json:"gitee_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email,omitempty"`
}

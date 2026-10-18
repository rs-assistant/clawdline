package cloudops

func init() {
	register(
		op{name: "squad.packages.preview", read: true},
		op{name: "squad.packages.adopt"},
		op{name: "squad.packages.export", read: true},
		op{name: "squad.packages.export.private"},
	)
}

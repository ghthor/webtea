.PHONY: go.work
go.work:
	rm -f go.work*
	go work init
	go work use ./gotty
	go work use ./
	go work sync

.PHONY: update_charmstack
update_charmstack:
	for p in wish lipgloss bubbletea bubbles log; do \
		go get charm.land/$$p/v2; \
	done

.PHONY: update_gotty
update_gotty:
	go get github.com/ghthor/gotty/v2@master

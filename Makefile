.PHONY: dev dist test

# dev runs the app with hot reload.
dev: desktop/node_modules
	cd desktop && pnpm dev

# dist builds desktop/dist/mac*/refine.app with the binary bundled.
dist: desktop/node_modules
	cd desktop && pnpm dist

test: desktop/node_modules
	go test ./...
	cd desktop && pnpm typecheck

desktop/node_modules: desktop/package.json desktop/pnpm-lock.yaml
	cd desktop && pnpm install --frozen-lockfile
	touch $@

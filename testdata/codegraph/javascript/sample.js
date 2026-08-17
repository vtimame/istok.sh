import fs from "fs"
import { join } from "path"

class Service {
	greet(name) {
		return fs.readFileSync(join("/tmp", `${name}`), "utf8")
	}
}

function run(name) {
	return new Service().greet(name)
}

import React from "react"
import { Props } from "./props"

interface WidgetProps {
	title: string
}

class Widget implements WidgetProps {
	title: string

	render() {
		return <div>{this.title}</div>
	}
}

function create(title: string): Widget {
	return new Widget()
}

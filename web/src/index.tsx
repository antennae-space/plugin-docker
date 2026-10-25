// The Docker tool, an external plugin of the IDE: its tool (the Compose stack of the project,
// the containers of the host, the disk), in a sandboxed frame; its backend speaks to Docker
// through the socket the IDE opens, and feeds the consoles of shells and long Compose actions.
import { view } from '@antennae-space/plugin-sdk/solid'
import { DockerTool } from './DockerTool'
import './docker.css'

ide.register({
  views: {
    docker: view(DockerTool),
  },
})

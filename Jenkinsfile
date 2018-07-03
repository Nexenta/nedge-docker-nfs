node('master') {
    docker.withServer('unix:///var/run/docker.sock') {
        stage('Git clone') {
            git url: 'https://github.com/Nexenta/nedge-docker-nfs-builders.git', branch: 'stable/v17-dev'
            sh """
                echo "Build number: ${BUILD_NUMBER}";
            """
        }
        stage('Build') {
            docker
                .image('solutions-team-jenkins-agent-ubuntu')
                .inside('--volumes-from solutions-team-jenkins-master') {
                    withCredentials([usernamePassword(
                        credentialsId: 'hub.docker-nedgeui',
                        passwordVariable: 'DOCKER_PASS',
                        usernameVariable: 'DOCKER_USER'
                    )]) {
                        sh """
                            pwd; \
                            ls -lha; \
                            docker --version; \
                            git --version; \
                            make --version; \
                            make push;
                        """
                    }
                }
        }
    }
}

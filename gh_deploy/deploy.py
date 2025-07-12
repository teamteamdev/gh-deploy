import fcntl
import logging
import subprocess
from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path

from gh_deploy.config import GitHttpSettings, Project, get_config
from gh_deploy.util import run_command

logger = logging.getLogger(__name__)


@contextmanager
def lock_directory(path: Path) -> Generator[None]:
    # We use lockfile in parent directory, otherwise git refuses to clone
    # in non-empty directory.
    absolute = path.absolute()
    with (absolute.parent / f".{path.name}-lock").open("w") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        yield


def clone_url(project: Project) -> str:
    settings = get_config().git

    if isinstance(settings, GitHttpSettings):
        return (
            f"https://{settings.username}:{settings.password}@github.com/{project.repo}"
        )

    return f"git@github.com:{project.repo}"


def deploy(project: Project, *, use_lfs: bool, default_timeout: int) -> None:
    project.path.mkdir(parents=True, exist_ok=True)
    full_branch = f"refs/heads/{project.branch}"

    with lock_directory(project.path):
        logger.info("Deploying %s#%s", project.repo, project.branch)
        try:
            existing_repo = (project.path / ".git").is_dir()

            if existing_repo:
                run_command(["git", "fetch", "origin", full_branch], cwd=project.path)
            else:
                run_command(
                    [
                        "git",
                        "clone",
                        clone_url(project),
                        ".",
                        "-b",
                        project.branch,
                    ],
                    cwd=project.path,
                )
                if use_lfs:
                    run_command(["git", "lfs", "install", "--local"], cwd=project.path)

            if use_lfs:
                run_command(
                    ["git", "lfs", "fetch", "origin", full_branch], cwd=project.path
                )

            if existing_repo:
                run_command(
                    [
                        "git",
                        "checkout",
                        "-B",
                        project.branch,
                        f"origin/{project.branch}",
                    ],
                    cwd=project.path,
                )

            if use_lfs:
                run_command(["git", "lfs", "checkout"], cwd=project.path)

            timeout = project.timeout or default_timeout
            if project.cmd is not None:
                run_command(
                    [project.cmd], cwd=project.path, timeout=timeout, shell=True
                )
            elif (project.path / "deploy.sh").is_file():
                run_command(["./deploy.sh"], cwd=dir, timeout=timeout)
            elif (project.path / "docker-compose.yml").is_file():
                run_command(["docker", "compose", "restart"], cwd=dir, timeout=timeout)
            else:
                logger.error("No idea how to deploy project in directory %s", dir)
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
            logger.exception("Can not deploy due to error")
            # TODO: notify
    logger.info("Successfully deployed %s#%s", project.repo, project.branch)

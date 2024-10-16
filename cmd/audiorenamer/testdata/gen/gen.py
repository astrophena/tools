import os
import random
import subprocess
from mutagen.easyid3 import EasyID3
from mutagen.flac import FLAC


def generate_fake_metadata():
    """Generates a dictionary of fake metadata."""
    artists = ["Fake Artist 1", "Fake Band", "Another Fake Artist"]
    albums = ["Fake Album 1", "Fake Album 2", "Greatest Fake Hits"]
    genres = ["Fake Genre", "Another Fake Genre", "Rock", "Pop"]

    return {
        "artist": random.choice(artists),
        "album": random.choice(albums),
        "title": f"Fake Track {random.randint(1, 10)}",
        "genre": random.choice(genres),
        "tracknumber": str(random.randint(1, 12)),
    }


def create_mp3(filename, metadata):
    """Creates an MP3 file with minimal silent audio and the given metadata."""
    # Generate 1 second of silence using ffmpeg.
    subprocess.run(
        [
            "ffmpeg",
            "-f",
            "lavfi",
            "-i",
            "anullsrc=channel_layout=stereo:sample_rate=44100",
            "-t",
            "1",
            "-c:a",
            "libmp3lame",
            filename,
        ],
        check=True,
    )

    # Add metadata using mutagen.
    audio = EasyID3(filename)
    for key, value in metadata.items():
        audio[key] = value
    audio.save()


def create_flac(filename, metadata):
    """Creates a FLAC file with minimal silent audio and the given metadata."""
    # Generate 1 second of silence using ffmpeg.
    subprocess.run(
        [
            "ffmpeg",
            "-f",
            "lavfi",
            "-i",
            "anullsrc=channel_layout=stereo:sample_rate=44100",
            "-t",
            "1",
            "-c:a",
            "flac",
            filename,
        ],
        check=True,
    )

    # Add metadata using mutagen.
    audio = FLAC(filename)
    for key, value in metadata.items():
        audio[key] = value
    audio.save()


# Create 5 files of each type.
for i in range(5):
    metadata = generate_fake_metadata()

    mp3_filename = f"fake_track_{i}.mp3"
    create_mp3(mp3_filename, metadata)

    flac_filename = f"fake_track_{i}.flac"
    create_flac(flac_filename, metadata)

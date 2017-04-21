package renter

import (
	"errors"
	"sync/atomic"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

func (r *Renter) DownloadSection(p *modules.RenterDownloadParameters) error {
	// Lookup the file associated with the nickname.
	lockID := r.mu.RLock()
	file, exists := r.files[p.Siapath]
	r.mu.RUnlock(lockID)
	if !exists {
		return errors.New("no file with that path")
	}

	// Build current contracts map.
	currentContracts := make(map[modules.NetAddress]types.FileContractID)
	for _, contract := range r.hostContractor.Contracts() {
		currentContracts[contract.NetAddress] = contract.ID
	}

	// Create the download object and add it to the queue.
	var d *download
	if p.Length == maxUint64 { // If whole file download.
		d = r.newDownload(file, p.DlWriter, currentContracts)
	} else {
		// Check whether offset and length is valid.
		if p.Offset < 0 || p.Offset+p.Length > file.size {
			emsg := "offset and length combination invalid, max byte is at index" + string(file.size-1)
			return errors.New(emsg)
		}
		d = r.newSectionDownload(file, p.DlWriter, currentContracts, p.Offset, p.Length)
	}

	lockID = r.mu.Lock()
	r.downloadQueue = append(r.downloadQueue, d)
	r.mu.Unlock(lockID)
	r.newDownloads <- d

	// Block until the download has completed.
	//
	// TODO: Eventually just return the channel to the error instead of the
	// error itself.
	select {
	case <-d.downloadFinished:
		return d.Err()
	case <-r.tg.StopChan():
		return errors.New("download interrupted by shutdown")
	}
}

// DownloadQueue returns the list of downloads in the queue.
func (r *Renter) DownloadQueue() []modules.DownloadInfo {
	lockID := r.mu.RLock()
	defer r.mu.RUnlock(lockID)

	// Order from most recent to least recent.
	downloads := make([]modules.DownloadInfo, len(r.downloadQueue))
	for i := range r.downloadQueue {
		d := r.downloadQueue[len(r.downloadQueue)-i-1]

		downloads[i] = modules.DownloadInfo{
			SiaPath:     d.siapath,
			Destination: d.destination,
			Filesize:    d.length,
			StartTime:   d.startTime,
		}
		downloads[i].Received = atomic.LoadUint64(&d.atomicDataReceived)

		if err := d.Err(); err != nil {
			downloads[i].Error = err.Error()
		}
	}
	return downloads
}

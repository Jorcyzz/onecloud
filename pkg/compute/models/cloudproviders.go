package models

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"yunion.io/x/jsonutils"
	"yunion.io/x/log"
	"yunion.io/x/onecloud/pkg/compute/options"
	"yunion.io/x/onecloud/pkg/httperrors"
	"yunion.io/x/onecloud/pkg/mcclient"
	"yunion.io/x/onecloud/pkg/mcclient/auth"
	"yunion.io/x/onecloud/pkg/mcclient/modules"
	"yunion.io/x/pkg/util/timeutils"
	"yunion.io/x/pkg/utils"

	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/cloudprovider"
)

const (
	CLOUD_PROVIDER_INIT         = "init"
	CLOUD_PROVIDER_CONNECTED    = "connected"
	CLOUD_PROVIDER_DISCONNECTED = "disconnected"
	CLOUD_PROVIDER_START_SYNC   = "start_sync"
	CLOUD_PROVIDER_SYNCING      = "syncing"

	CLOUD_PROVIDER_VMWARE = "VMware"
	CLOUD_PROVIDER_ALIYUN = "Aliyun"
	CLOUD_PROVIDER_AZURE  = "Azure"
)

type SCloudproviderManager struct {
	db.SEnabledStatusStandaloneResourceBaseManager
	SInfrastructureManager
}

var CloudproviderManager *SCloudproviderManager

func init() {
	CloudproviderManager = &SCloudproviderManager{SEnabledStatusStandaloneResourceBaseManager: db.NewEnabledStatusStandaloneResourceBaseManager(SCloudprovider{}, "cloudproviders_tbl", "cloudprovider", "cloudproviders")}
}

type SCloudprovider struct {
	db.SEnabledStatusStandaloneResourceBase
	SInfrastructure

	AccessUrl string `width:"64" charset:"ascii" nullable:"true" list:"admin" update:"admin" create:"admin_optional"`
	// Hostname string `width:"64" charset:"ascii" nullable:"true"` // Column(VARCHAR(64, charset='ascii'), nullable=False)
	// port = Column(Integer, nullable=False)
	Account string `width:"128" charset:"ascii" nullable:"false" list:"admin" create:"admin_required"` // Column(VARCHAR(64, charset='ascii'), nullable=False)
	Secret  string `width:"256" charset:"ascii" nullable:"false" list:"admin" create:"admin_required"` // Column(VARCHAR(256, charset='ascii'), nullable=False)

	CloudaccountId string `width:"36" charset:"ascii" nullable:"false" list:"user" create:"required" key_index:"true"`

	ProjectId string `name:"tenant_id" width:"128" charset:"ascii" nullable:"true" list:"admin"`

	LastSync time.Time `get:"admin" list:"admin"` // = Column(DateTime, nullable=True)

	Version string `width:"32" charset:"ascii" nullable:"true" list:"admin"` // Column(VARCHAR(32, charset='ascii'), nullable=True)

	Sysinfo jsonutils.JSONObject `get:"admin"` // Column(JSONEncodedDict, nullable=True)

	Provider string `width:"64" charset:"ascii" list:"admin" create:"admin_required"`
}

func (self *SCloudprovider) ValidateDeleteCondition(ctx context.Context) error {
	if self.Enabled {
		return httperrors.NewInvalidStatusError("provider is enabled")
	}
	usage := self.getUsage()
	if !usage.isEmpty() {
		return httperrors.NewNotEmptyError("Not an empty cloud provider")
	}
	return self.SEnabledStatusStandaloneResourceBase.ValidateDeleteCondition(ctx)
}

func (self *SCloudprovider) GetGuestCount() int {
	sq := HostManager.Query("id").Equals("manager_id", self.Id)
	return GuestManager.Query().In("host_id", sq).Count()
}

func (self *SCloudprovider) GetHostCount() int {
	return HostManager.Query().Equals("manager_id", self.Id).Count()
}

func (self *SCloudprovider) getVpcCount() int {
	return VpcManager.Query().Equals("manager_id", self.Id).Count()
}

func (self *SCloudprovider) getStorageCount() int {
	return StorageManager.Query().Equals("manager_id", self.Id).Count()
}

func (self *SCloudprovider) getStoragecacheCount() int {
	return StoragecacheManager.Query().Equals("manager_id", self.Id).Count()
}

func (self *SCloudprovider) getEipCount() int {
	return ElasticipManager.Query().Equals("manager_id", self.Id).Count()
}

func (self *SCloudprovider) ValidateUpdateData(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data *jsonutils.JSONDict) (*jsonutils.JSONDict, error) {
	return self.SEnabledStatusStandaloneResourceBase.ValidateUpdateData(ctx, userCred, query, data)
}

func (self *SCloudproviderManager) ValidateCreateData(ctx context.Context, userCred mcclient.TokenCredential, ownerProjId string, query jsonutils.JSONObject, data *jsonutils.JSONDict) (*jsonutils.JSONDict, error) {
	return nil, httperrors.NewUnsupportOperationError("Not support create cloudprovider, please considir create cloudaccount")
}

func (self *SCloudprovider) getPassword() (string, error) {
	return utils.DescryptAESBase64(self.Id, self.Secret)
}

func (self *SCloudprovider) CanSync() bool {
	if self.Status == CLOUD_PROVIDER_SYNCING {
		if self.LastSync.IsZero() || time.Now().Sub(self.LastSync) > 900*time.Second {
			return true
		} else {
			return false
		}
	} else {
		return true
	}
}

func (self *SCloudprovider) SyncProject() (err error) {
	projectId := ""
	if len(self.ProjectId) == 0 && len(self.Name) > 0 && self.Provider == CLOUD_PROVIDER_AZURE {
		s := auth.GetAdminSession(options.Options.Region, "")
		if project, err := modules.Projects.GetByName(s, self.Name, nil); err == nil {
			if projectId, err = project.GetString("id"); err != nil {
				return err
			}
		} else if strings.Index(err.Error(), "404 NotFoundError") > 0 {
			if project, err := modules.Projects.Create(s, jsonutils.Marshal(map[string]string{"name": self.Name})); err != nil {
				return err
			} else if projectId, err = project.GetString("id"); err != nil {
				return err
			}
		} else {
			return err
		}
		if len(projectId) > 0 {
			if _, err := self.GetModelManager().TableSpec().Update(self, func() error {
				self.ProjectId = projectId
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type SSyncRange struct {
	Force    bool
	FullSync bool
	Region   []string
	Zone     []string
	Host     []string
}

func (sr *SSyncRange) NeedSyncInfo() bool {
	if sr.FullSync {
		return true
	}
	if sr.Region != nil && len(sr.Region) > 0 {
		return true
	}
	if sr.Zone != nil && len(sr.Zone) > 0 {
		return true
	}
	if sr.Host != nil && len(sr.Host) > 0 {
		return true
	}
	return false
}

func (sr *SSyncRange) normalizeRegionIds() error {
	for i := 0; i < len(sr.Region); i += 1 {
		obj, err := CloudregionManager.FetchByIdOrName(nil, sr.Region[i])
		if err != nil {
			if err == sql.ErrNoRows {
				return httperrors.NewResourceNotFoundError("Region %s not found", sr.Region[i])
			} else {
				return err
			}
		}
		sr.Region[i] = obj.GetId()
	}
	return nil
}

func (sr *SSyncRange) normalizeZoneIds() error {
	for i := 0; i < len(sr.Zone); i += 1 {
		obj, err := ZoneManager.FetchByIdOrName(nil, sr.Zone[i])
		if err != nil {
			if err == sql.ErrNoRows {
				return httperrors.NewResourceNotFoundError("Zone %s not found", sr.Zone[i])
			} else {
				return err
			}
		}
		sr.Zone[i] = obj.GetId()
	}
	return nil
}

func (sr *SSyncRange) normalizeHostIds() error {
	for i := 0; i < len(sr.Host); i += 1 {
		obj, err := HostManager.FetchByIdOrName(nil, sr.Host[i])
		if err != nil {
			if err == sql.ErrNoRows {
				return httperrors.NewResourceNotFoundError("Host %s not found", sr.Host[i])
			} else {
				return err
			}
		}
		sr.Host[i] = obj.GetId()
	}
	return nil
}

func (sr *SSyncRange) Normalize() error {
	if sr.Region != nil && len(sr.Region) > 0 {
		err := sr.normalizeRegionIds()
		if err != nil {
			return err
		}
	}
	if sr.Zone != nil && len(sr.Zone) > 0 {
		err := sr.normalizeZoneIds()
		if err != nil {
			return err
		}
	}
	if sr.Host != nil && len(sr.Host) > 0 {
		err := sr.normalizeHostIds()
		if err != nil {
			return err
		}
	}
	return nil
}

func (self *SCloudprovider) AllowPerformSync(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) bool {
	return userCred.IsSystemAdmin()
}

func (self *SCloudprovider) PerformSync(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) (jsonutils.JSONObject, error) {
	if !self.Enabled {
		return nil, httperrors.NewInvalidStatusError("Cloudprovider disabled")
	}
	syncRange := SSyncRange{}
	err := data.Unmarshal(&syncRange)
	if err != nil {
		return nil, httperrors.NewInputParameterError("invalid input %s", err)
	}
	if self.CanSync() || syncRange.Force {
		err = self.StartSyncCloudProviderInfoTask(ctx, userCred, &syncRange, "")
	}
	return nil, err
}

func (self *SCloudprovider) StartSyncCloudProviderInfoTask(ctx context.Context, userCred mcclient.TokenCredential, syncRange *SSyncRange, parentTaskId string) error {
	if err := self.SyncProject(); err != nil {
		log.Errorf("Sync cloudprovider project error: %v", err)
	}
	params := jsonutils.NewDict()
	if syncRange != nil {
		params.Add(jsonutils.Marshal(syncRange), "sync_range")
	}
	task, err := taskman.TaskManager.NewTask(ctx, "CloudProviderSyncInfoTask", self, userCred, params, parentTaskId, "", nil)
	if err != nil {
		log.Errorf("startSyncCloudProviderInfoTask newTask error %s", err)
		return err
	}
	task.ScheduleRun(nil)
	return nil
}

func (self *SCloudprovider) MarkStartSync(userCred mcclient.TokenCredential) {
	_, err := self.GetModelManager().TableSpec().Update(self, func() error {
		self.LastSync = timeutils.UtcNow()
		return nil
	})
	if err != nil {
		log.Errorf("Fail tp update last_sync %s", err)
		return
	}
	self.SetStatus(userCred, CLOUD_PROVIDER_START_SYNC, "")
}

type SAccount struct {
	AccessUrl string
	Account   string
	Secret    string
}

func (self *SCloudprovider) getCloudaccount() (*SCloudaccount, error) {
	if cloudaccount := CloudaccountManager.FetchCloudaccountById(self.CloudaccountId); cloudaccount != nil {
		return cloudaccount, nil
	}
	return nil, fmt.Errorf("Failed to find cloud account for cloud provider %s", self.Name)
}

func (self *SCloudprovider) getAccount() (*SAccount, error) {
	if cloudaccount, err := self.getCloudaccount(); err != nil {
		return nil, err
	} else {
		account := SAccount{AccessUrl: cloudaccount.AccessUrl, Account: cloudaccount.Account}
		if passwd, err := cloudaccount.getPassword(); err != nil {
			return nil, err
		} else {
			account.Secret = passwd
		}
		if len(self.Account) > 0 && self.Account != cloudaccount.Account {
			account.Account = fmt.Sprintf("%s/%s", account.Account, self.Account)
		}
		return &account, nil
	}

}

func (self *SCloudprovider) GetDriver() (cloudprovider.ICloudProvider, error) {
	if !self.Enabled {
		return nil, fmt.Errorf("Cloud provider is not enabled")
	}

	account, err := self.getAccount()
	if err != nil {
		return nil, err
	}
	return cloudprovider.GetProvider(self.Id, self.Name, account.AccessUrl, account.Account, account.Secret, self.Provider)
}

func (self *SCloudprovider) SaveSysInfo(info jsonutils.JSONObject) {
	self.GetModelManager().TableSpec().Update(self, func() error {
		self.Sysinfo = info
		return nil
	})
}

func (manager *SCloudproviderManager) FetchCloudproviderById(providerId string) *SCloudprovider {
	providerObj, err := manager.FetchById(providerId)
	if err != nil {
		log.Errorf("%s", err)
		return nil
	}
	return providerObj.(*SCloudprovider)
}

func (manager *SCloudproviderManager) FetchCloudproviderByIdOrName(providerId string) *SCloudprovider {
	providerObj, err := manager.FetchByIdOrName(nil, providerId)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Errorf("%s", err)
		}
		return nil
	}
	return providerObj.(*SCloudprovider)
}

type SCloudproviderUsage struct {
	GuestCount        int
	HostCount         int
	VpcCount          int
	StorageCount      int
	StorageCacheCount int
	EipCount          int
}

func (usage *SCloudproviderUsage) isEmpty() bool {
	if usage.HostCount > 0 {
		return false
	}
	if usage.VpcCount > 0 {
		return false
	}
	if usage.StorageCount > 0 {
		return false
	}
	if usage.StorageCacheCount > 0 {
		return false
	}
	if usage.EipCount > 0 {
		return false
	}
	return true
}

func (self *SCloudprovider) getUsage() *SCloudproviderUsage {
	usage := SCloudproviderUsage{}
	usage.GuestCount = self.GetGuestCount()
	usage.HostCount = self.GetHostCount()
	usage.VpcCount = self.getVpcCount()
	usage.StorageCount = self.getStorageCount()
	usage.StorageCacheCount = self.getStoragecacheCount()
	usage.EipCount = self.getEipCount()

	return &usage
}

func (self *SCloudprovider) getMoreDetails(extra *jsonutils.JSONDict) *jsonutils.JSONDict {
	extra.Update(jsonutils.Marshal(self.getUsage()))
	return extra
}

func (self *SCloudprovider) GetCustomizeColumns(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) *jsonutils.JSONDict {
	extra := self.SEnabledStatusStandaloneResourceBase.GetCustomizeColumns(ctx, userCred, query)
	return self.getMoreDetails(extra)
}

func (self *SCloudprovider) GetExtraDetails(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) *jsonutils.JSONDict {
	extra := self.SEnabledStatusStandaloneResourceBase.GetExtraDetails(ctx, userCred, query)
	return self.getMoreDetails(extra)
}

func (manager *SCloudproviderManager) InitializeData() error {
	// move vmware info from vcenter to cloudprovider
	vcenters := make([]SVCenter, 0)
	q := VCenterManager.Query()
	err := db.FetchModelObjects(VCenterManager, q, &vcenters)
	if err != nil {
		return err
	}
	for _, vc := range vcenters {
		_, err := CloudproviderManager.FetchById(vc.Id)
		if err != nil {
			if err == sql.ErrNoRows {
				err = manager.migrateVCenterInfo(&vc)
				if err != nil {
					log.Errorf("migrateVcenterInfo fail %s", err)
					return err
				}
				_, err = VCenterManager.TableSpec().Update(&vc, func() error {
					return vc.MarkDelete()
				})
				if err != nil {
					log.Errorf("delete vcenter record fail %s", err)
					return err
				}
			} else {
				log.Errorf("fetch cloudprovider fail %s", err)
				return err
			}
		} else {
			log.Debugf("vcenter info has been migrate into cloudprovider")
		}
	}
	return nil
}

func (manager *SCloudproviderManager) migrateVCenterInfo(vc *SVCenter) error {
	cp := SCloudprovider{}
	cp.SetModelManager(manager)

	cp.Id = vc.Id
	cp.Name = db.GenerateName(manager, "", vc.Name)
	cp.Status = vc.Status
	cp.AccessUrl = fmt.Sprintf("https://%s:%d", vc.Hostname, vc.Port)
	cp.Account = vc.Account
	cp.Secret = vc.Password
	cp.LastSync = vc.LastSync
	cp.Sysinfo = vc.Sysinfo
	cp.Provider = CLOUD_PROVIDER_VMWARE

	return manager.TableSpec().Insert(&cp)
}

func (self *SCloudprovider) GetBalance() (float64, error) {
	driver, err := self.GetDriver()
	if err != nil {
		return 0.0, err
	}
	return driver.GetBalance()
}

func (self *SCloudprovider) AllowGetDetailsBalance(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) bool {
	return userCred.IsSystemAdmin()
}

func (self *SCloudprovider) GetDetailsBalance(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) (jsonutils.JSONObject, error) {
	balance, err := self.GetBalance()
	if err != nil {
		return nil, httperrors.NewGeneralError(err)
	}
	ret := jsonutils.NewDict()
	ret.Add(jsonutils.NewFloat(balance), "balance")
	return ret, nil
}
